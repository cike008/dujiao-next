package consumer

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/queue"

	"github.com/hibiken/asynq"
)

func (c *Consumer) handleTeamGenieSyncFulfilled(ctx context.Context, task *asynq.Task) error {
	if c == nil || task == nil {
		logger.Debugw("worker_teamgenie_sync_skip_nil", "consumer_nil", c == nil, "task_nil", task == nil)
		return nil
	}
	if c.TeamGenieSyncService == nil {
		logger.Debugw("worker_teamgenie_sync_skip_service_nil")
		return nil
	}

	var payload queue.TeamGenieSyncFulfilledPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		logger.Warnw("worker_teamgenie_sync_unmarshal_failed", "error", err)
		return err
	}
	if payload.OrderID == 0 {
		logger.Debugw("worker_teamgenie_sync_skip_invalid_payload", "order_id", payload.OrderID)
		return nil
	}

	startedAt := time.Now()
	retryCount, _ := asynq.GetRetryCount(ctx)
	maxRetry, _ := asynq.GetMaxRetry(ctx)
	logger.Infow("worker_teamgenie_sync_started",
		"order_id", payload.OrderID,
		"retry_count", retryCount,
		"max_retry", maxRetry,
	)

	order, err := c.OrderStore.GetByID(payload.OrderID)
	if err != nil {
		logger.Warnw("worker_teamgenie_sync_fetch_order_failed", "order_id", payload.OrderID, "error", err)
		return err
	}
	if order == nil {
		logger.Debugw("worker_teamgenie_sync_skip_order_not_found", "order_id", payload.OrderID)
		return nil
	}
	if order.Fulfillment == nil {
		logger.Debugw("worker_teamgenie_sync_skip_fulfillment_not_found", "order_id", payload.OrderID, "order_no", order.OrderNo)
		return nil
	}

	if err := c.TeamGenieSyncService.SyncFulfilled(order); err != nil {
		logger.Warnw("worker_teamgenie_sync_failed",
			"order_id", order.ID,
			"order_no", order.OrderNo,
			"fulfillment_id", order.Fulfillment.ID,
			"fulfillment_status", order.Fulfillment.Status,
			"retry_count", retryCount,
			"max_retry", maxRetry,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"error", err,
		)
		return err
	}

	logger.Infow("worker_teamgenie_sync_succeeded",
		"order_id", order.ID,
		"order_no", order.OrderNo,
		"fulfillment_id", order.Fulfillment.ID,
		"fulfillment_status", order.Fulfillment.Status,
		"retry_count", retryCount,
		"max_retry", maxRetry,
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"cards", len(strings.Fields(strings.ReplaceAll(strings.TrimSpace(order.Fulfillment.Payload), "\n", " "))),
	)
	return nil
}
