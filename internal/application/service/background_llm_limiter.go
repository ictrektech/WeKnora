package service

import (
	"context"
	"sync"
)

var (
	backgroundLLMOnce sync.Once
	backgroundLLMSem  chan struct{}
)

func backgroundLLMCapacity(main, reserved int) int {
	if main <= 0 || reserved <= 0 {
		return 0
	}
	if main <= reserved {
		return 1
	}
	return main - reserved
}

func acquireBackgroundLLMSlot(ctx context.Context) (func(), error) {
	backgroundLLMOnce.Do(func() {
		capacity := backgroundLLMCapacity(
			envPositiveInt("WEKNORA_MAIN_QA_MODEL_CONCURRENCY", 0),
			envPositiveInt("WEKNORA_CHAT_RESERVED_CONCURRENCY", 0),
		)
		if capacity > 0 {
			backgroundLLMSem = make(chan struct{}, capacity)
		}
	})
	if backgroundLLMSem == nil {
		return func() {}, nil
	}
	select {
	case backgroundLLMSem <- struct{}{}:
		return func() { <-backgroundLLMSem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
