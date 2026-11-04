package storage

import (
	"context"
	"time"

	"github.com/solidtrace/event_store/pkg/logger"
)

// LocalCacheHoursInterval is how often EventStore reads Console's
// local_cache_hours, so that a change on Settings → Instance moves parts
// within about a minute, with no restart.
const LocalCacheHoursInterval = time.Minute

// localCacheHoursTimeout bounds one read and the ALTERs it makes.
const localCacheHoursTimeout = 30 * time.Second

// LocalCacheHoursSetting reads how long Logs and Spans stay on local disk
// (ConsoleSettings.LocalCacheHours).
type LocalCacheHoursSetting interface {
	LocalCacheHours() (int, error)
}

// LocalCacheFollower keeps the day tables' TTL at Console's
// local_cache_hours: it reads the setting at start and then every interval,
// and sets it (ClickHouse.UseLocalCacheHours), which touches ClickHouse only
// when the hours changed. It's a ticker of its own, not part of the cleanup,
// which runs hourly: a change should apply within a minute, and a read of
// Console's SQLite and a comparison in memory cost next to nothing. With no
// bucket it does nothing.
type LocalCacheFollower struct {
	ch      *ClickHouse
	setting LocalCacheHoursSetting
	every   time.Duration
	stop    chan struct{}
	done    chan struct{}
}

// FollowLocalCacheHours applies setting's hours to ch at once, then every
// interval until Stop.
func FollowLocalCacheHours(ch *ClickHouse, setting LocalCacheHoursSetting, every time.Duration) *LocalCacheFollower {
	f := &LocalCacheFollower{ch: ch, setting: setting, every: every, stop: make(chan struct{}), done: make(chan struct{})}
	if !ch.bucket {
		close(f.done)
		return f
	}
	f.apply()
	go f.run()
	return f
}

func (f *LocalCacheFollower) run() {
	defer close(f.done)
	ticker := time.NewTicker(f.every)
	defer ticker.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-ticker.C:
			f.apply()
		}
	}
}

// apply reads the hours and sets them. A read or a set that fails keeps the
// hours last set, and is logged; the next tick tries again.
func (f *LocalCacheFollower) apply() {
	hours, err := f.setting.LocalCacheHours()
	if err != nil {
		logger.L.Error("Reading the hours Logs stay on local disk failed, so they stay as they were",
			"hours", f.ch.LocalCacheHours(), "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), localCacheHoursTimeout)
	defer cancel()
	if err := f.ch.UseLocalCacheHours(ctx, hours); err != nil {
		logger.L.Error("Setting the hours Logs stay on local disk failed, so they stay as they were",
			"hours", f.ch.LocalCacheHours(), "wanted", hours, "error", err)
	}
}

// Stop stops reading the setting, and waits for a read under way.
func (f *LocalCacheFollower) Stop() {
	select {
	case <-f.done:
		return
	default:
	}
	close(f.stop)
	<-f.done
}
