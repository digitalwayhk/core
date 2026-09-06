package mq

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const redisLifecycleGeneration = "1"

var redisLifecycleOwnerSequence atomic.Uint64

var redisLifecycleMaxFrontierScript = redis.NewScript(`
local current = redis.call("HGET", KEYS[1], ARGV[1])
if not current then
  redis.call("HSET", KEYS[1], ARGV[1], ARGV[2])
  return ARGV[2]
end
local function parts(id)
  local dash = string.find(id, "-")
  return tonumber(string.sub(id, 1, dash - 1)), tonumber(string.sub(id, dash + 1))
end
local cm, cs = parts(current)
local nm, ns = parts(ARGV[2])
if nm > cm or (nm == cm and ns > cs) then
  redis.call("HSET", KEYS[1], ARGV[1], ARGV[2])
  return ARGV[2]
end
return current
`)

var redisLifecycleReclaimScript = redis.NewScript(`
if redis.call("GET", KEYS[3]) ~= ARGV[1] then
  return redis.error_reply("LIFECYCLE_OWNER_LOST")
end
if redis.call("HGET", KEYS[2], "fingerprint") ~= ARGV[2] then
  return redis.error_reply("LIFECYCLE_POLICY_CONFLICT")
end
if redis.call("HGET", KEYS[2], "generation") ~= ARGV[3] then
  return redis.error_reply("LIFECYCLE_GENERATION_CONFLICT")
end

local function less(a, b)
  local ad = string.find(a, "-")
  local bd = string.find(b, "-")
  local am = tonumber(string.sub(a, 1, ad - 1))
  local bm = tonumber(string.sub(b, 1, bd - 1))
  if am ~= bm then return am < bm end
  local as = tonumber(string.sub(a, ad + 1))
  local bs = tonumber(string.sub(b, bd + 1))
  return as < bs
end

local function successor(id)
  local dash = string.find(id, "-")
  local millis = string.sub(id, 1, dash - 1)
  local sequence = tonumber(string.sub(id, dash + 1)) + 1
  return millis .. "-" .. tostring(sequence)
end

local groups = redis.call("XINFO", "GROUPS", KEYS[1])
for argi = 6, #ARGV do
  local required = ARGV[argi]
  local found = false
  local delivered = "0-0"
  for _, group in ipairs(groups) do
    local name = nil
    local last = nil
    for i = 1, #group, 2 do
      if group[i] == "name" then name = group[i + 1] end
      if group[i] == "last-delivered-id" then last = group[i + 1] end
    end
    if name == required then
      found = true
      delivered = last
      break
    end
  end
  if not found then return redis.error_reply("LIFECYCLE_REQUIRED_GROUP_MISSING") end
  if less(successor(delivered), ARGV[4]) then return redis.error_reply("LIFECYCLE_GROUP_CURSOR_REGRESSED") end
  local pending = redis.call("XPENDING", KEYS[1], required)
  if pending[1] > 0 and less(pending[2], ARGV[4]) then
    return redis.error_reply("LIFECYCLE_PENDING_BEFORE_FRONTIER")
  end
end

local entries = redis.call("XRANGE", KEYS[1], "-", "(" .. ARGV[4], "COUNT", ARGV[5])
if #entries == 0 then return 0 end
local ids = {}
for _, entry in ipairs(entries) do table.insert(ids, entry[1]) end
return redis.call("XDEL", KEYS[1], unpack(ids))
`)

// LifecycleCapabilities 声明 Redis Streams 的实际生命周期能力。
// XADD 成功只确认 Broker 接受，不伪装成与服务端 AOF 无关的落盘确认。
func (*RedisStreamProvider) LifecycleCapabilities() LifecycleCapabilities {
	return LifecycleCapabilities{
		PublishAck:       PublishAckBrokerAccepted,
		RequiredGroups:   true,
		Retry:            true,
		DeadLetter:       true,
		SafeReclaim:      true,
		RetainedMessages: true,
		RetainedBytes:    true,
		Pending:          true,
		Lag:              true,
		OldestAge:        true,
	}
}

// EnsureLifecycle 校验策略指纹并预建所有必需消费组。
func (r *RedisStreamProvider) EnsureLifecycle(ctx context.Context, policy LifecyclePolicy) error {
	if r == nil || r.client == nil {
		return ErrNotConnected
	}
	policy = policy.Normalize()
	if err := policy.Validate(); err != nil {
		return err
	}
	streamKey := r.streamKey(policy.Subject)
	metaKey := r.lifecycleMetaKey(policy.Subject)
	existingGroups, err := r.redisLifecycleGroups(ctx, streamKey)
	if err != nil {
		return err
	}
	for _, required := range policy.RequiredGroups {
		if _, exists := existingGroups[required.Name]; !exists || required.Start != StartFromNew {
			continue
		}
		enrollment, metaErr := r.client.HGet(ctx, metaKey, redisLifecycleEnrollmentField(required.Name)).Result()
		if metaErr == redis.Nil || enrollment == "" {
			return fmt.Errorf("%w: existing Redis group %q has unknown new-only enrollment", ErrLifecycleStateUncertain, required.Name)
		}
		if metaErr != nil {
			return metaErr
		}
	}

	fingerprint := policy.Fingerprint()
	set, err := r.client.HSetNX(ctx, metaKey, "fingerprint", fingerprint).Result()
	if err != nil {
		return err
	}
	if !set {
		actual, getErr := r.client.HGet(ctx, metaKey, "fingerprint").Result()
		if getErr != nil {
			return getErr
		}
		if actual != fingerprint {
			return fmt.Errorf("%w: Redis subject %q", ErrLifecyclePolicyConflict, policy.Subject)
		}
	}
	if err := r.client.HSetNX(ctx, metaKey, "generation", redisLifecycleGeneration).Err(); err != nil {
		return err
	}

	for _, required := range policy.RequiredGroups {
		info, exists := existingGroups[required.Name]
		if !exists {
			start := "0"
			if required.Start == StartFromNew {
				start = "$"
			}
			err = r.client.XGroupCreateMkStream(ctx, streamKey, required.Name, start).Err()
			if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
				return fmt.Errorf("redis-stream: create lifecycle group %q: %w", required.Name, err)
			}
			groups, groupErr := r.redisLifecycleGroups(ctx, streamKey)
			if groupErr != nil {
				return groupErr
			}
			info, exists = groups[required.Name]
			if !exists {
				return fmt.Errorf("%w: Redis group %q was not created", ErrLifecycleStateUncertain, required.Name)
			}
		}
		enrollment := "0-0"
		if required.Start == StartFromNew {
			enrollment = info.LastDeliveredID
			if enrollment == "" {
				enrollment = "0-0"
			}
		}
		if err := r.client.HSetNX(ctx, metaKey, redisLifecycleEnrollmentField(required.Name), enrollment).Err(); err != nil {
			return err
		}
		completed := enrollment
		if enrollment != "0-0" {
			completed = streamIDSuccessor(enrollment)
		}
		if err := r.client.HSetNX(ctx, metaKey, redisLifecycleCompletedField(required.Name), completed).Err(); err != nil {
			return err
		}
	}
	return nil
}

// InspectLifecycle 从 Redis 原生 group/PEL 状态计算所有必需组的连续安全前沿。
func (r *RedisStreamProvider) InspectLifecycle(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
	if r == nil || r.client == nil {
		return LifecycleSnapshot{}, ErrNotConnected
	}
	policy = policy.Normalize()
	streamKey := r.streamKey(policy.Subject)
	metaKey := r.lifecycleMetaKey(policy.Subject)
	fingerprint, err := r.client.HGet(ctx, metaKey, "fingerprint").Result()
	if err != nil {
		return LifecycleSnapshot{}, fmt.Errorf("%w: Redis lifecycle metadata missing", ErrLifecycleStateUncertain)
	}
	if fingerprint != policy.Fingerprint() {
		return LifecycleSnapshot{}, fmt.Errorf("%w: Redis subject %q", ErrLifecyclePolicyConflict, policy.Subject)
	}
	stream, err := r.client.XInfoStream(ctx, streamKey).Result()
	if err != nil {
		return LifecycleSnapshot{}, err
	}
	groups, err := r.redisLifecycleGroups(ctx, streamKey)
	if err != nil {
		return LifecycleSnapshot{}, err
	}

	retained := stream.Length
	pendingTotal := int64(0)
	backlog := int64(0)
	backlogKnown := true
	safeFrontier := ""
	groupSnapshots := make([]ConsumerGroupSnapshot, 0, len(policy.RequiredGroups))
	state := LifecycleStateOK
	for _, required := range policy.RequiredGroups {
		info, exists := groups[required.Name]
		if !exists {
			return LifecycleSnapshot{}, fmt.Errorf("%w: Redis required group %q is missing", ErrLifecycleStateUncertain, required.Name)
		}
		enrollment, metaErr := r.client.HGet(ctx, metaKey, redisLifecycleEnrollmentField(required.Name)).Result()
		if metaErr != nil || enrollment == "" {
			return LifecycleSnapshot{}, fmt.Errorf("%w: Redis group %q enrollment is missing", ErrLifecycleStateUncertain, required.Name)
		}
		pending, pendingErr := r.client.XPending(ctx, streamKey, required.Name).Result()
		if pendingErr != nil {
			return LifecycleSnapshot{}, pendingErr
		}
		frontier := info.LastDeliveredID
		if frontier == "" {
			frontier = "0-0"
		}
		if pending.Count > 0 && pending.Lower != "" {
			frontier = pending.Lower
		} else if frontier != "0-0" {
			frontier = streamIDSuccessor(frontier)
		}
		enrollmentBoundary := enrollment
		if enrollment != "0-0" {
			enrollmentBoundary = streamIDSuccessor(enrollment)
		}
		if streamIDCompare(frontier, enrollmentBoundary) < 0 {
			frontier = enrollmentBoundary
		}
		completedField := redisLifecycleCompletedField(required.Name)
		previous, previousErr := r.client.HGet(ctx, metaKey, completedField).Result()
		if previousErr != nil && previousErr != redis.Nil {
			return LifecycleSnapshot{}, previousErr
		}
		if previous != "" && streamIDCompare(frontier, previous) < 0 {
			return LifecycleSnapshot{}, fmt.Errorf("%w: Redis group %q frontier regressed", ErrLifecycleStateUncertain, required.Name)
		}
		stored, storeErr := redisLifecycleMaxFrontierScript.Run(ctx, r.client, []string{metaKey}, completedField, frontier).Text()
		if storeErr != nil {
			return LifecycleSnapshot{}, storeErr
		}
		frontier = stored
		if safeFrontier == "" || streamIDCompare(frontier, safeFrontier) < 0 {
			safeFrontier = frontier
		}
		pendingCount := pending.Count
		pendingTotal += pendingCount
		var lag *int64
		if info.Lag >= 0 {
			lagValue := info.Lag
			lag = &lagValue
			groupBacklog := pendingCount + lagValue
			if groupBacklog > backlog {
				backlog = groupBacklog
			}
		} else {
			backlogKnown = false
			state = LifecycleStatePartial
		}
		groupSnapshots = append(groupSnapshots, ConsumerGroupSnapshot{
			Name: required.Name, State: LifecycleStateOK, Pending: &pendingCount, Lag: lag,
			EnrollmentFrontier: enrollment, CompletedFrontier: frontier,
		})
	}

	cutoff := time.Now().Add(-policy.Retention.MinAge)
	if policy.Retention.MinAge == 0 {
		cutoff = cutoff.Add(time.Millisecond)
	}
	cutoffID := fmt.Sprintf("%d-0", cutoff.UnixMilli())
	if safeFrontier == "" || streamIDCompare(cutoffID, safeFrontier) < 0 {
		safeFrontier = cutoffID
	}
	retainedBytes, memoryErr := r.client.MemoryUsage(ctx, streamKey).Result()
	var retainedBytesPtr *int64
	if memoryErr == nil {
		retainedBytesPtr = &retainedBytes
	} else {
		state = LifecycleStatePartial
	}
	var oldestAge *time.Duration
	if stream.Length > 0 && stream.FirstEntry.ID != "" {
		if millis, _, parseErr := parseStreamID(stream.FirstEntry.ID); parseErr == nil {
			age := time.Since(time.UnixMilli(millis))
			if age < 0 {
				age = 0
			}
			oldestAge = &age
		}
	}
	var backlogPtr *int64
	if backlogKnown {
		backlogPtr = &backlog
	}
	return LifecycleSnapshot{
		Subject: policy.Subject, PolicyFingerprint: fingerprint,
		ObservedAt: time.Now(), State: state,
		RetainedMessages: &retained, RetainedBytes: retainedBytesPtr,
		BacklogMessages: backlogPtr, PendingMessages: &pendingTotal,
		OldestAge: oldestAge, SafeFrontier: safeFrontier, Groups: groupSnapshots,
	}, nil
}

// ReclaimLifecycle 在同一 Redis Lua 中重新校验组游标、pending、owner 和策略后有界删除。
func (r *RedisStreamProvider) ReclaimLifecycle(
	ctx context.Context,
	policy LifecyclePolicy,
	snapshot LifecycleSnapshot,
) (ReclaimResult, error) {
	started := time.Now()
	if r == nil || r.client == nil {
		return ReclaimResult{}, ErrNotConnected
	}
	policy = policy.Normalize()
	if err := validateLifecycleSnapshot(policy, snapshot); err != nil {
		return ReclaimResult{}, err
	}
	if snapshot.SafeFrontier == "" || snapshot.SafeFrontier == "0-0" {
		return ReclaimResult{Duration: time.Since(started)}, nil
	}
	streamKey := r.streamKey(policy.Subject)
	metaKey := r.lifecycleMetaKey(policy.Subject)
	ownerKey := r.lifecycleOwnerKey(policy.Subject)
	owner := fmt.Sprintf("%d-%d", time.Now().UnixNano(), redisLifecycleOwnerSequence.Add(1))
	leaseTTL := 3 * policy.Reclaim.TimeBudget
	if leaseTTL < time.Second {
		leaseTTL = time.Second
	}
	acquired, err := r.client.SetNX(ctx, ownerKey, owner, leaseTTL).Result()
	if err != nil {
		return ReclaimResult{}, err
	}
	if !acquired {
		return ReclaimResult{Duration: time.Since(started)}, nil
	}
	defer func() {
		_ = redisOwnerReleaseScript.Run(context.Background(), r.client, []string{ownerKey}, owner).Err()
	}()
	args := []interface{}{
		owner, policy.Fingerprint(), redisLifecycleGeneration,
		snapshot.SafeFrontier, policy.Reclaim.BatchSize,
	}
	for _, required := range policy.RequiredGroups {
		args = append(args, required.Name)
	}
	count, err := redisLifecycleReclaimScript.Run(ctx, r.client, []string{streamKey, metaKey, ownerKey}, args...).Int64()
	if err != nil {
		return ReclaimResult{}, fmt.Errorf("%w: Redis reclaim fence: %v", ErrLifecycleStateUncertain, err)
	}
	return ReclaimResult{
		Reclaimed: count, Duration: time.Since(started),
		BudgetExhausted: count >= int64(policy.Reclaim.BatchSize),
	}, nil
}

func (r *RedisStreamProvider) redisLifecycleGroups(ctx context.Context, streamKey string) (map[string]redis.XInfoGroup, error) {
	items, err := r.client.XInfoGroups(ctx, streamKey).Result()
	if err != nil {
		if strings.Contains(err.Error(), "no such key") {
			return map[string]redis.XInfoGroup{}, nil
		}
		return nil, err
	}
	groups := make(map[string]redis.XInfoGroup, len(items))
	for _, item := range items {
		groups[item.Name] = item
	}
	return groups, nil
}

func (r *RedisStreamProvider) lifecycleMetaKey(subject string) string {
	return r.prefix + ":lifecycle:" + redisLifecycleSubjectHash(subject) + ":meta"
}

func (r *RedisStreamProvider) lifecycleOwnerKey(subject string) string {
	return r.prefix + ":lifecycle:" + redisLifecycleSubjectHash(subject) + ":owner"
}

func redisLifecycleSubjectHash(subject string) string {
	digest := sha256.Sum256([]byte(subject))
	return hex.EncodeToString(digest[:8])
}

func redisLifecycleEnrollmentField(group string) string {
	return "group:" + group + ":enrollment"
}

func redisLifecycleCompletedField(group string) string {
	return "group:" + group + ":completed"
}

func parseStreamID(id string) (int64, uint64, error) {
	parts := strings.SplitN(id, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid Redis stream ID %q", id)
	}
	millis, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	sequence, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return millis, sequence, nil
}

func streamIDCompare(left, right string) int {
	leftMillis, leftSequence, leftErr := parseStreamID(left)
	rightMillis, rightSequence, rightErr := parseStreamID(right)
	if leftErr != nil || rightErr != nil {
		return strings.Compare(left, right)
	}
	if leftMillis < rightMillis {
		return -1
	}
	if leftMillis > rightMillis {
		return 1
	}
	if leftSequence < rightSequence {
		return -1
	}
	if leftSequence > rightSequence {
		return 1
	}
	return 0
}

func streamIDSuccessor(id string) string {
	millis, sequence, err := parseStreamID(id)
	if err != nil {
		return id
	}
	return fmt.Sprintf("%d-%d", millis, sequence+1)
}
