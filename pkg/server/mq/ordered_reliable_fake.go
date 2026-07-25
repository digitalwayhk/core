package mq

import (
	"context"
	"sync"
	"time"
)

// FakeOrderedReliableProvider 是 provider-neutral ordered-reliable 验收用内存实现。
// 同 OrderingKey 串行且失败阻断；不同 key 可并行。
type FakeOrderedReliableProvider struct {
	mu       sync.Mutex
	subs     map[string][]*fakeOrderedSub
	queues   map[string][]fakeOrderedMsg // key = subject + "\x00" + orderingKey
	inflight map[string]bool
	seq      int
}

type fakeOrderedMsg struct {
	id          string
	subject     string
	data        []byte
	orderingKey string
}

type fakeOrderedSub struct {
	group   string
	handler func(*Message) error
}

// NewFakeOrderedReliableProvider 构造空 fake provider。
func NewFakeOrderedReliableProvider() *FakeOrderedReliableProvider {
	return &FakeOrderedReliableProvider{
		subs:     make(map[string][]*fakeOrderedSub),
		queues:   make(map[string][]fakeOrderedMsg),
		inflight: make(map[string]bool),
	}
}

func (f *FakeOrderedReliableProvider) Name() string                  { return "fake-ordered-reliable" }
func (f *FakeOrderedReliableProvider) Connect(context.Context) error { return nil }
func (f *FakeOrderedReliableProvider) Close() error                  { return nil }
func (f *FakeOrderedReliableProvider) Health(context.Context) error  { return nil }
func (f *FakeOrderedReliableProvider) OrderedReliableInfo() OrderedReliableCapability {
	return DefaultOrderedReliableCapability()
}

func (f *FakeOrderedReliableProvider) Publish(_ context.Context, subject string, data []byte, opts *PublishOptions) error {
	orderingKey := "_default"
	idem := ""
	if opts != nil {
		if opts.OrderingKey != "" {
			orderingKey = opts.OrderingKey
		}
		idem = opts.IdempotencyKey
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	id := idem
	if id == "" {
		id = time.Now().Format("150405.000000") + ":" + string(rune('a'+f.seq%26))
	}
	qkey := subject + "\x00" + orderingKey
	f.queues[qkey] = append(f.queues[qkey], fakeOrderedMsg{
		id: id, subject: subject, data: append([]byte(nil), data...), orderingKey: orderingKey,
	})
	f.dispatchLocked(subject, orderingKey)
	return nil
}

func (f *FakeOrderedReliableProvider) Subscribe(context.Context, string, func(*Message)) (func(), error) {
	return func() {}, nil
}

func (f *FakeOrderedReliableProvider) SubscribeReliable(
	_ context.Context,
	subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
) (func(), error) {
	if handler == nil || options.Group == "" {
		return nil, ErrReliableSubscribeUnsupported
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	sub := &fakeOrderedSub{group: options.Group, handler: handler}
	f.subs[subject] = append(f.subs[subject], sub)
	for qkey := range f.queues {
		if len(qkey) > len(subject) && qkey[:len(subject)] == subject && qkey[len(subject)] == 0 {
			f.dispatchLocked(subject, qkey[len(subject)+1:])
		}
	}
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		list := f.subs[subject]
		out := list[:0]
		for _, item := range list {
			if item != sub {
				out = append(out, item)
			}
		}
		f.subs[subject] = out
	}, nil
}

func (f *FakeOrderedReliableProvider) dispatchLocked(subject, orderingKey string) {
	qkey := subject + "\x00" + orderingKey
	if f.inflight[qkey] || len(f.queues[qkey]) == 0 || len(f.subs[subject]) == 0 {
		return
	}
	msg := f.queues[qkey][0]
	handler := f.subs[subject][0].handler
	f.inflight[qkey] = true
	go func() {
		message := &Message{
			ID:      msg.id,
			Subject: msg.subject,
			Data:    msg.data,
			Ack:     func() error { return nil },
		}
		err := handler(message)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.inflight[qkey] = false
		if err != nil {
			// 失败阻断：保留队头，短暂后重试（模拟 pending redelivery）。
			time.AfterFunc(20*time.Millisecond, func() {
				f.mu.Lock()
				defer f.mu.Unlock()
				f.dispatchLocked(subject, orderingKey)
			})
			return
		}
		if len(f.queues[qkey]) > 0 && f.queues[qkey][0].id == msg.id {
			f.queues[qkey] = f.queues[qkey][1:]
		}
		f.dispatchLocked(subject, orderingKey)
	}()
}
