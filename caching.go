package postcodeapi

import (
	"encoding/json"
	"log"
	"time"

	"github.com/tidwall/buntdb"
)

// struct for cache
type cache struct {
	ApiFullResponse
	CachedAt time.Time `json:"cached_at"`
}

type cacheDb struct {
	bunt *buntdb.DB
}

// init cache db
func (api *ApiClientSettings) InitDb() cacheDb {
	// check if db file is specified
	// if not, use default file name
	if api.CacheFile == "" {
		api.CacheFile = "./data/pcapi_cache.db"
	}

	// open db file
	db, err := buntdb.Open(api.CacheFile)
	if err != nil {
		log.Fatal(err)
	}
	return cacheDb{bunt: db}
}

// function to save to cache
func (c *cacheDb) SaveToCache(key string, value cache) {
	// type cache to json
	valueJson, err := json.Marshal(value)
	if err != nil {
		log.Println(err)
		return
	}
	c.bunt.Update(func(tx *buntdb.Tx) error {
		// save to cache
		tx.Set(key, string(valueJson), nil)
		return nil
	})
}

// function to get from cache (returns cache struct) or nil
// get from cache by key (postcode+number)
func (c *cacheDb) GetFromCache(key string) *cache {
	var value cache
	c.bunt.View(func(tx *buntdb.Tx) error {
		val, err := tx.Get(key)
		if err != nil {
			return err
		}
		// convert json to struct
		err = json.Unmarshal([]byte(val), &value)
		if err != nil {
			return err
		}
		return nil
	})
	return &value
}

// function to save api limits info to cache
func (api *ApiClientSettings) SaveToCache() {
	// convert to json
	json, err := json.Marshal(api.ApiInfo)
	if err != nil {
		log.Println(err)
		return
	}
	// save to buntdb
	api.Cache.bunt.Update(func(tx *buntdb.Tx) error {
		tx.Set("api_info", string(json), nil)
		// set caching time
		tx.Set("api_info_cached_at", time.Now().Format(time.RFC3339), nil)
		return nil
	})
}

// get api limits info caching time
func (api *ApiClientSettings) GetCachingTime() time.Time {
	var cachingTime time.Time
	api.Cache.bunt.View(func(tx *buntdb.Tx) error {
		val, err := tx.Get("api_info_cached_at")
		if err != nil {
			return err
		}
		cachingTime, err = time.Parse(time.RFC3339, val)
		if err != nil {
			return err
		}
		return nil
	})
	return cachingTime
}

// function to get api limits info from cache
func (api *ApiClientSettings) GetFromCache() {
	api.Cache.bunt.View(func(tx *buntdb.Tx) error {
		val, err := tx.Get("api_info")
		if err != nil {
			return err
		}
		// convert json to struct
		err = json.Unmarshal([]byte(val), &api.ApiInfo)
		if err != nil {
			return err
		}
		return nil
	})
}
