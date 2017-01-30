package mdata

import (
	"sync"
	"time"

	"github.com/raintank/metrictank/mdata/cache"
	"github.com/raintank/worldping-api/pkg/log"
	"github.com/zhangxinngang/murmur"
)

const NUM_SHARDS = uint32(256)

type Shard struct {
	sync.RWMutex
	Metrics map[string]*AggMetric
}

func GetShardBucket(key string) uint32 {
	return murmur.Murmur3([]byte(key)) % NUM_SHARDS
}

type AggMetrics struct {
	store          Store
	cachePusher    cache.CachePusher
	Shards         []Shard
	chunkSpan      uint32
	numChunks      uint32
	aggSettings    []AggSetting // for now we apply the same settings to all AggMetrics. later we may want to have different settings.
	chunkMaxStale  uint32
	metricMaxStale uint32
	ttl            uint32
	gcInterval     time.Duration
}

func NewAggMetrics(store Store, cachePusher cache.CachePusher, chunkSpan, numChunks, chunkMaxStale, metricMaxStale uint32, ttl uint32, gcInterval time.Duration, aggSettings []AggSetting) *AggMetrics {
	ms := AggMetrics{
		store:          store,
		cachePusher:    cachePusher,
		Shards:         make([]Shard, NUM_SHARDS),
		chunkSpan:      chunkSpan,
		numChunks:      numChunks,
		aggSettings:    aggSettings,
		chunkMaxStale:  chunkMaxStale,
		metricMaxStale: metricMaxStale,
		ttl:            ttl,
		gcInterval:     gcInterval,
	}

	for i := uint32(0); i < NUM_SHARDS; i++ {
		ms.Shards[i] = Shard{
			Metrics: make(map[string]*AggMetric),
		}
	}
	// gcInterval = 0 can be useful in tests
	if gcInterval > 0 {
		go ms.GC()
	}
	return &ms
}

// periodically scan chunks and close any that have not received data in a while
func (ms *AggMetrics) GC() {
	for {
		unix := time.Duration(time.Now().UnixNano())
		diff := ms.gcInterval - (unix % ms.gcInterval)
		time.Sleep(diff + time.Minute)
		log.Info("checking for stale chunks that need persisting.")
		now := uint32(time.Now().Unix())
		chunkMinTs := now - (now % ms.chunkSpan) - uint32(ms.chunkMaxStale)
		metricMinTs := now - (now % ms.chunkSpan) - uint32(ms.metricMaxStale)

		for i := range ms.Shards {
			shard := &ms.Shards[i]
			// as this is the only goroutine that can delete from ms.Metrics
			// we only need to lock long enough to get the list of actives metrics.
			// it doesn't matter if new metrics are added while we iterate this list.
			shard.RLock()
			keys := make([]string, 0, len(shard.Metrics))
			for k := range shard.Metrics {
				keys = append(keys, k)
			}
			shard.RUnlock()
			for _, key := range keys {
				gcMetric.Inc()
				shard.RLock()
				a := shard.Metrics[key]
				shard.RUnlock()
				if stale := a.GC(chunkMinTs, metricMinTs); stale {
					log.Info("metric %s is stale. Purging data from memory.", key)
					shard.Lock()
					delete(shard.Metrics, key)
					metricsActive.Dec()
					shard.Unlock()
				}
			}
		}

	}
}

func (ms *AggMetrics) Get(key string) (Metric, bool) {
	shard := &ms.Shards[GetShardBucket(key)]
	shard.RLock()
	m, ok := shard.Metrics[key]
	shard.RUnlock()
	return m, ok
}

func (ms *AggMetrics) GetOrCreate(key string) Metric {
	shard := &ms.Shards[GetShardBucket(key)]
	shard.Lock()
	m, ok := shard.Metrics[key]
	if !ok {
		m = NewAggMetric(ms.store, ms.cachePusher, key, ms.chunkSpan, ms.numChunks, ms.ttl, ms.aggSettings...)
		shard.Metrics[key] = m
		metricsActive.Inc()
	}
	shard.Unlock()
	return m
}

func (ms *AggMetrics) AggSettings() []AggSetting {
	return ms.aggSettings
}
