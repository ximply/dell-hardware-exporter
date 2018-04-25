package cache

import (
        "sync"
        "time"
        "github.com/muesli/cache2go"
)

var cm *CacheManager
var once sync.Once
var g_cache = cache2go.Cache("dellhw")

func GetInstance() *CacheManager {
    once.Do(func() {
        cm = &CacheManager {}
    })
    return cm
}

type CacheManager struct {}

type DellhwCache struct {
        Text string
        MoreData []byte
}

func (p CacheManager) Add(key interface{}, lifeSpan time.Duration, data interface{}) {
    g_cache.Add(key, lifeSpan, data)
}

func (p CacheManager) Value(key interface{}, args ...interface{}) (ok bool, dc *DellhwCache) {
        item, err := g_cache.Value(key, args)
        if err == nil {
                return true, item.Data().(*DellhwCache)
        }

        return false, nil
}