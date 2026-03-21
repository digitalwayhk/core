-- ============================================================
-- 查询用户订单及其在各服务中的状态记录
-- 说明：ETHUSDT 部分来自 future_order_model.pair 字段
--       如需查其他交易对，全局替换 ETHUSDT 即可
-- ============================================================
SELECT
    -- ── 用户订单基础信息 ──────────────────────────────────────
    f.id                    AS order_id,
    f.user_id,
    f.pair,
    f.status,
    f.side,
    f.order_type,
    f.amount,
    f.price,
    f.mark_price,
    f.volume,
    f.avg_price,
    f.leverage,
    f.margin,
    f.position_side,
    f.margin_mode,
    f.order_time,
    f.is_waiting,
    f.error_msg,

    -- ── 来源表标记 ───────────────────────────────────────────
    CASE f.status
        WHEN 'NEW'      THEN 'order_model (运行中)'
        WHEN 'CANCELED' THEN 'order_cancel_model (已撤单)'
        ELSE                 'order_done_model (已完成)'
    END AS source_table,

    -- ── positions 服务侧数据 ─────────────────────────────────
    COALESCE(po_new.id,    po_cancel.order_id, po_done.id)    AS pos_order_ref,
    COALESCE(po_new.send_status, po_cancel.send_status, po_done.send_status) AS pos_send_status,
    COALESCE(po_new.send_error,  po_cancel.send_error,  po_done.send_error)  AS pos_send_error,
    COALESCE(po_new.position_id, po_cancel.position_id, po_done.position_id) AS pos_position_id,
    po_cancel.cancel_amount     AS pos_cancel_amount,
    po_cancel.cancel_freeze_fund,
    po_cancel.cancel_trade,
    po_done.cancel_amount       AS pos_done_cancel_amount,

    -- ── trades 服务侧数据 ────────────────────────────────────
    COALESCE(tr_new.id,    tr_cancel.order_id, tr_done.id)    AS tr_order_ref,
    tr_new.match_state      AS tr_match_state,   -- 0:未成交 1:部分成交 2:全成交 3:已撤单
    tr_cancel.cancel_amount AS tr_cancel_amount,
    tr_cancel.cancel_freeze,
    tr_cancel.cancel_freeze_error,
    tr_cancel.cancel_error,
    tr_done.match_state     AS tr_done_match_state

FROM bitzoom_users.future_order_model f

-- ══════════════════════════════════════════════════════════
-- NEW 状态：订单仍在撮合引擎中运行
-- ══════════════════════════════════════════════════════════
LEFT JOIN bitzoom_futures_positions_ETHUSDT.order_model po_new
    ON  f.status = 'NEW'
    AND po_new.id = f.id

LEFT JOIN bitzoom_futures_trades_ETHUSDT.order_model tr_new
    ON  f.status = 'NEW'
    AND tr_new.id = f.id

-- ══════════════════════════════════════════════════════════
-- CANCELED 状态：订单已撤销，记录移入 cancel 表
-- ══════════════════════════════════════════════════════════
LEFT JOIN bitzoom_futures_trades_ETHUSDT.order_cancel_model tr_cancel
    ON  f.status = 'CANCELED'
    AND tr_cancel.order_id = f.id

LEFT JOIN bitzoom_futures_positions_ETHUSDT.order_cancel_model po_cancel
    ON  f.status = 'CANCELED'
    AND po_cancel.order_id = f.id

-- ══════════════════════════════════════════════════════════
-- 其他状态（PARTIALLY_FILLED / FILLED / ERROR）
-- 订单已完结，记录移入 done 表
-- ══════════════════════════════════════════════════════════
LEFT JOIN bitzoom_futures_positions_ETHUSDT.order_done_model po_done
    ON  f.status NOT IN ('NEW', 'CANCELED')
    AND po_done.id = f.id

LEFT JOIN bitzoom_futures_trades_ETHUSDT.order_done_record_model tr_done
    ON  f.status NOT IN ('NEW', 'CANCELED')
    AND tr_done.id = f.id

WHERE f.pair = 'ETHUSDT'
  -- AND f.user_id = 'xxx'    -- 可选：按用户过滤
  -- AND f.status = 'NEW'     -- 可选：按状态过滤
ORDER BY f.order_time DESC
LIMIT 100;

-- ============================================================
-- 数据一致性核查：验证 future_order_model 是否正确同步到
-- positions 和 trades 服务库
-- 
-- 核查逻辑：
--   NEW       → 应在 positions.order_model        AND trades.order_model
--   CANCELED  → 应在 positions.order_cancel_model AND trades.order_cancel_model
--   其他      → 应在 positions.order_done_model   AND trades.order_done_record_model
--
-- 结果字段说明：
--   pos_count / tr_count  = 0 表示缺失，>1 表示重复，1 为正常
--   amount_match          = 0 表示金额不一致
-- ============================================================
SELECT
    f.id                                        AS order_id,
    f.user_id,
    f.pair,
    f.status,
    f.amount                                    AS f_amount,
    f.order_time,

    -- ── 期望所在的表 ─────────────────────────────────────────
    CASE f.status
        WHEN 'NEW'      THEN 'order_model'
        WHEN 'CANCELED' THEN 'order_cancel_model'
        ELSE                 'order_done_model / order_done_record_model'
    END AS expected_table,

    -- ── positions 服务：该订单在对应表中的行数 ───────────────
    COALESCE((
        SELECT COUNT(*)
        FROM bitzoom_futures_positions_ETHUSDT.order_model po
        WHERE f.status = 'NEW' AND po.id = f.id
    ), 0) +
    COALESCE((
        SELECT COUNT(*)
        FROM bitzoom_futures_positions_ETHUSDT.order_cancel_model poc
        WHERE f.status = 'CANCELED' AND poc.order_id = f.id
    ), 0) +
    COALESCE((
        SELECT COUNT(*)
        FROM bitzoom_futures_positions_ETHUSDT.order_done_model pod
        WHERE f.status NOT IN ('NEW', 'CANCELED') AND pod.id = f.id
    ), 0)                                       AS pos_count,  -- 期望为 1

    -- ── trades 服务：该订单在对应表中的行数 ──────────────────
    COALESCE((
        SELECT COUNT(*)
        FROM bitzoom_futures_trades_ETHUSDT.order_model tr
        WHERE f.status = 'NEW' AND tr.id = f.id
    ), 0) +
    COALESCE((
        SELECT COUNT(*)
        FROM bitzoom_futures_trades_ETHUSDT.order_cancel_model trc
        WHERE f.status = 'CANCELED' AND trc.order_id = f.id
    ), 0) +
    COALESCE((
        SELECT COUNT(*)
        FROM bitzoom_futures_trades_ETHUSDT.order_done_record_model trd
        WHERE f.status NOT IN ('NEW', 'CANCELED') AND trd.id = f.id
    ), 0)                                       AS tr_count,   -- 期望为 1

    -- ── positions 金额一致性（NEW 状态）──────────────────────
    CASE
        WHEN f.status = 'NEW' THEN
            CASE WHEN EXISTS (
                SELECT 1 FROM bitzoom_futures_positions_ETHUSDT.order_model po
                WHERE po.id = f.id AND po.amount = f.amount
            ) THEN 1 ELSE 0 END
        ELSE NULL
    END                                         AS pos_amount_match,  -- NULL=不适用, 1=一致, 0=不一致

    -- ── trades 金额一致性（NEW 状态）────────────────────────-
    CASE
        WHEN f.status = 'NEW' THEN
            CASE WHEN EXISTS (
                SELECT 1 FROM bitzoom_futures_trades_ETHUSDT.order_model tr
                WHERE tr.id = f.id AND tr.amount = f.amount
            ) THEN 1 ELSE 0 END
        ELSE NULL
    END                                         AS tr_amount_match,   -- NULL=不适用, 1=一致, 0=不一致

    -- ── 综合状态 ─────────────────────────────────────────────
    CASE
        WHEN
            -- pos 和 tr 都恰好有 1 条
            (COALESCE((SELECT COUNT(*) FROM bitzoom_futures_positions_ETHUSDT.order_model      WHERE f.status = 'NEW'                     AND id = f.id), 0) +
             COALESCE((SELECT COUNT(*) FROM bitzoom_futures_positions_ETHUSDT.order_cancel_model WHERE f.status = 'CANCELED'               AND order_id = f.id), 0) +
             COALESCE((SELECT COUNT(*) FROM bitzoom_futures_positions_ETHUSDT.order_done_model  WHERE f.status NOT IN ('NEW','CANCELED')   AND id = f.id), 0)) = 1
            AND
            (COALESCE((SELECT COUNT(*) FROM bitzoom_futures_trades_ETHUSDT.order_model          WHERE f.status = 'NEW'                     AND id = f.id), 0) +
             COALESCE((SELECT COUNT(*) FROM bitzoom_futures_trades_ETHUSDT.order_cancel_model   WHERE f.status = 'CANCELED'               AND order_id = f.id), 0) +
             COALESCE((SELECT COUNT(*) FROM bitzoom_futures_trades_ETHUSDT.order_done_record_model WHERE f.status NOT IN ('NEW','CANCELED') AND id = f.id), 0)) = 1
        THEN '✅ 正常'
        WHEN
            (COALESCE((SELECT COUNT(*) FROM bitzoom_futures_positions_ETHUSDT.order_model      WHERE f.status = 'NEW'                     AND id = f.id), 0) +
             COALESCE((SELECT COUNT(*) FROM bitzoom_futures_positions_ETHUSDT.order_cancel_model WHERE f.status = 'CANCELED'               AND order_id = f.id), 0) +
             COALESCE((SELECT COUNT(*) FROM bitzoom_futures_positions_ETHUSDT.order_done_model  WHERE f.status NOT IN ('NEW','CANCELED')   AND id = f.id), 0)) = 0
            OR
            (COALESCE((SELECT COUNT(*) FROM bitzoom_futures_trades_ETHUSDT.order_model          WHERE f.status = 'NEW'                     AND id = f.id), 0) +
             COALESCE((SELECT COUNT(*) FROM bitzoom_futures_trades_ETHUSDT.order_cancel_model   WHERE f.status = 'CANCELED'               AND order_id = f.id), 0) +
             COALESCE((SELECT COUNT(*) FROM bitzoom_futures_trades_ETHUSDT.order_done_record_model WHERE f.status NOT IN ('NEW','CANCELED') AND id = f.id), 0)) = 0
        THEN '❌ 缺失'
        ELSE '⚠️ 重复'
    END                                         AS sync_status

FROM bitzoom_users.future_order_model f
WHERE f.pair = 'ETHUSDT'
  -- AND f.user_id = 'xxx'   -- 可选：按用户过滤
  -- AND sync_status != '✅ 正常'  -- MySQL 不支持在 WHERE 中引用别名，用 HAVING 过滤异常:
HAVING sync_status != '✅ 正常'   -- 只显示有问题的订单，去掉此行则显示全部
ORDER BY f.order_time DESC
LIMIT 200;

-- ── 汇总统计（快速了解整体同步质量）────────────────────────
SELECT
    f.status,
    COUNT(*)                                    AS total_orders,
    SUM(CASE WHEN
        (COALESCE((SELECT COUNT(*) FROM bitzoom_futures_positions_ETHUSDT.order_model      WHERE f.status = 'NEW'                     AND id = f.id), 0) +
         COALESCE((SELECT COUNT(*) FROM bitzoom_futures_positions_ETHUSDT.order_cancel_model WHERE f.status = 'CANCELED'               AND order_id = f.id), 0) +
         COALESCE((SELECT COUNT(*) FROM bitzoom_futures_positions_ETHUSDT.order_done_model  WHERE f.status NOT IN ('NEW','CANCELED')   AND id = f.id), 0)) = 1
        AND
        (COALESCE((SELECT COUNT(*) FROM bitzoom_futures_trades_ETHUSDT.order_model          WHERE f.status = 'NEW'                     AND id = f.id), 0) +
         COALESCE((SELECT COUNT(*) FROM bitzoom_futures_trades_ETHUSDT.order_cancel_model   WHERE f.status = 'CANCELED'               AND order_id = f.id), 0) +
         COALESCE((SELECT COUNT(*) FROM bitzoom_futures_trades_ETHUSDT.order_done_record_model WHERE f.status NOT IN ('NEW','CANCELED') AND id = f.id), 0)) = 1
    THEN 1 ELSE 0 END)                          AS synced_ok,
    SUM(CASE WHEN
        (COALESCE((SELECT COUNT(*) FROM bitzoom_futures_positions_ETHUSDT.order_model      WHERE f.status = 'NEW'                     AND id = f.id), 0) +
         COALESCE((SELECT COUNT(*) FROM bitzoom_futures_positions_ETHUSDT.order_cancel_model WHERE f.status = 'CANCELED'               AND order_id = f.id), 0) +
         COALESCE((SELECT COUNT(*) FROM bitzoom_futures_positions_ETHUSDT.order_done_model  WHERE f.status NOT IN ('NEW','CANCELED')   AND id = f.id), 0)) = 0
        OR
        (COALESCE((SELECT COUNT(*) FROM bitzoom_futures_trades_ETHUSDT.order_model          WHERE f.status = 'NEW'                     AND id = f.id), 0) +
         COALESCE((SELECT COUNT(*) FROM bitzoom_futures_trades_ETHUSDT.order_cancel_model   WHERE f.status = 'CANCELED'               AND order_id = f.id), 0) +
         COALESCE((SELECT COUNT(*) FROM bitzoom_futures_trades_ETHUSDT.order_done_record_model WHERE f.status NOT IN ('NEW','CANCELED') AND id = f.id), 0)) = 0
    THEN 1 ELSE 0 END)                          AS missing,
    COUNT(*) - SUM(CASE WHEN
        (COALESCE((SELECT COUNT(*) FROM bitzoom_futures_positions_ETHUSDT.order_model      WHERE f.status = 'NEW'                     AND id = f.id), 0) +
         COALESCE((SELECT COUNT(*) FROM bitzoom_futures_positions_ETHUSDT.order_cancel_model WHERE f.status = 'CANCELED'               AND order_id = f.id), 0) +
         COALESCE((SELECT COUNT(*) FROM bitzoom_futures_positions_ETHUSDT.order_done_model  WHERE f.status NOT IN ('NEW','CANCELED')   AND id = f.id), 0)) = 1
        AND
        (COALESCE((SELECT COUNT(*) FROM bitzoom_futures_trades_ETHUSDT.order_model          WHERE f.status = 'NEW'                     AND id = f.id), 0) +
         COALESCE((SELECT COUNT(*) FROM bitzoom_futures_trades_ETHUSDT.order_cancel_model   WHERE f.status = 'CANCELED'               AND order_id = f.id), 0) +
         COALESCE((SELECT COUNT(*) FROM bitzoom_futures_trades_ETHUSDT.order_done_record_model WHERE f.status NOT IN ('NEW','CANCELED') AND id = f.id), 0)) = 1
    THEN 1 ELSE 0 END)                          AS not_ok
FROM bitzoom_users.future_order_model f
WHERE f.pair = 'ETHUSDT'
GROUP BY f.status
ORDER BY f.status;
