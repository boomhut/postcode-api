package postcodeapi

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// struct for api settings
type ApiClientSettings struct {
	ApiEndpoint    string
	ApiBearerToken string
	ApiInfo        ApiLimitsInfo
	Cache          cacheDb
	CacheTtl       time.Duration
	CacheFile      string
}

// struct for api limits info
type ApiLimitsInfo struct {
	MaxRequestsPerMinute   int `json:"max_requests_per_minute"`  // get from api response header X-RateLimit-Limit
	RemainingRequests      int `json:"remaining_requests"`       // get from api response header X-RateLimit-Remaining
	MaxRequestsPerDay      int `json:"max_requests_per_day"`     // get from api response header X-API-Limit
	RemainingRequestsToday int `json:"remaining_requests_today"` // get from api response header X-API-Remaining

}

// struct for api response (short)
type ApiShortResponse struct {
	Street string `json:"street"`
	City   string `json:"city"`
}

// struct for 'full' api response
type ApiFullResponse struct {
	Postcode     string `json:"postcode,omitempty"`
	Number       int    `json:"number,omitempty"`
	Street       string `json:"street,omitempty"`
	City         string `json:"city,omitempty"`
	Municipality string `json:"municipality,omitempty"`
	Province     string `json:"province,omitempty"`
	Geo          struct {
		Lat float64 `json:"lat,omitempty"`
		Lon float64 `json:"lon,omitempty"`
	} `json:"geo,omitempty"`
	Error   string           `json:"error,omitempty"`
	ApiInfo ApiLimitInfoJson `json:"apiInfo,omitempty"`
}

// struct to output api limit info as json
type ApiLimitInfoJson struct {
	MaxRequestsPerMinute   int           `json:"maxRequestsPerMinute,omitempty"`
	RemainingRequests      int           `json:"remainingRequests,omitempty"`
	MaxRequestsPerDay      int           `json:"maxRequestsPerDay,omitempty"`
	RemainingRequestsToday int           `json:"remainingRequestsToday,omitempty"`
	CachingTime            time.Time     `json:"cachingTime,omitempty"`
	TimeSinceLastCache     time.Duration `json:"timeSinceLastCache,omitempty"` // time since last cache in seconds
}

// create new apiClientSettings with cache
func NewApiClientSettings(apiBearerToken string, cacheFile string, cacheTtl time.Duration) *ApiClientSettings {

	// apiEndpoint = "https://postcode.tech/api/v1/" // default api endpoint
	apiEndpoint := "https://postcode.tech/api/v1/"

	api := &ApiClientSettings{
		ApiEndpoint:    apiEndpoint,
		ApiBearerToken: apiBearerToken,
		CacheTtl:       cacheTtl,
		CacheFile:      cacheFile,
	}
	// set cachedb
	api.Cache = api.initDb()

	// get api limits info from cache
	api.GetFromCache()

	return api

}

// function to fetch from api
func (api *ApiClientSettings) fetchFromApi(postcode string, number string) *ApiFullResponse {
	// fetch from api
	// prepare request
	req, err := http.NewRequest("GET", api.ApiEndpoint+"postcode/full?postcode="+postcode+"&number="+number, nil)
	if err != nil {
		log.Println(err)
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+api.ApiBearerToken)
	req.Header.Set("User-Agent", "sw-core/2.0")

	// send request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println(err)
		return nil
	}
	defer resp.Body.Close()

	// update rate limit info
	api.ApiInfo.MaxRequestsPerMinute, _ = strconv.Atoi(resp.Header.Get("X-RateLimit-Limit"))
	api.ApiInfo.RemainingRequests, _ = strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining"))
	api.ApiInfo.MaxRequestsPerDay, _ = strconv.Atoi(resp.Header.Get("X-API-Limit"))
	api.ApiInfo.RemainingRequestsToday, _ = strconv.Atoi(resp.Header.Get("X-API-Remaining"))

	// save api info to cache
	api.SaveToCache()

	// check response status code (200 = ok) and return nil if not ok (e.g. 404)
	// 404 = postcode / number combination not found, so no need to cache this
	if resp.StatusCode != 200 {
		// check if 404
		if resp.StatusCode == 404 {

			// save to cache, so we don't have to fetch from api again
			api.Cache.SaveToCache(postcode+number, cache{ApiFullResponse{Error: "unknown combination"}, time.Now()})
			return &ApiFullResponse{Error: "unknown combination"}
		}
		// if 429 (too many requests) return error (so we don't cache this)
		if resp.StatusCode == 429 {
			return &ApiFullResponse{Error: "too many requests"}
		}
		// return api error
		return &ApiFullResponse{Error: "api error"}
	}

	// read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
		return nil
	}

	// convert json to struct
	var apiResponse ApiFullResponse
	err = json.Unmarshal(body, &apiResponse)
	if err != nil {
		log.Println(err)
		return nil
	}
	return &apiResponse
}

// function to get from api or cache
func (api *ApiClientSettings) GetPostcodeInfo(postcode string, number string) *ApiFullResponse {
	// check cache
	cached := api.Cache.GetFromCache(postcode + number)
	// if cache is not empty and not expired
	if cached != nil && time.Since(cached.CachedAt) < api.CacheTtl {
		// return from cache

		// check if cached api response is valid (e.g. not 404)
		if cached.ApiFullResponse.Error == "" {
			return &cached.ApiFullResponse
		} else {
			// check ttl of cache
			if time.Since(cached.CachedAt) < api.CacheTtl/6 {
				// // log for debugging
				// log.Println("cache hit (error)")
				// // serving this error from cache, because it's not expired for x more days
				// log.Printf("Serving this error from cache, because it's not expired for %v more days", api.cacheTtl/6/24/60/60)

				// if cache is not expired, return cached error
				return &cached.ApiFullResponse
			}
		}
	}
	// fetch from api
	apiResponse := api.fetchFromApi(postcode, number)
	// save to cache, if valid response
	if apiResponse != nil {
		api.Cache.SaveToCache(postcode+number, cache{*apiResponse, time.Now()})
		return apiResponse
	}
	return nil
}

// function to get postcode and number from string (e.g. 6931XE130 or 6931XE 130)
func (api *ApiClientSettings) GetPostcodeInfoFromString(postcodeNumber string) *ApiFullResponse {
	// use regex to get postcode and number
	re := regexp.MustCompile(`([0-9]{4}[A-Z]{2})([0-9]+)`)
	matches := re.FindStringSubmatch(postcodeNumber)
	if len(matches) == 3 {
		return api.GetPostcodeInfo(matches[1], matches[2])
	}
	return nil
}

// function to get short info from api (PIS = Postcode Info Short)
func (api *ApiClientSettings) GetPIS(postcode string, number string) *ApiShortResponse {
	// check cache
	cached := api.Cache.GetFromCache(postcode + number)
	// if cache is not empty and not expired
	if cached != nil && time.Since(cached.CachedAt) < api.CacheTtl {
		// return from cache
		return &ApiShortResponse{cached.ApiFullResponse.Street, cached.ApiFullResponse.City}
	}
	// fetch from api
	apiResponse := api.fetchFromApi(postcode, number)
	// save to cache, if valid response
	if apiResponse != nil {
		api.Cache.SaveToCache(postcode+number, cache{*apiResponse, time.Now()})
		return &ApiShortResponse{apiResponse.Street, apiResponse.City}
	}
	return nil
}

// function to get api limit info and caching time as json string
func (api *ApiClientSettings) GetApiLimitInfoJson() string {

	// check if api limit info is available
	if api.ApiInfo.MaxRequestsPerMinute == 0 {
		// try loading api limit info from cache
		api.GetFromCache()

		// check if api limit info is still not available
		if api.ApiInfo.MaxRequestsPerMinute == 0 {
			log.Println("API limit info not available")
			return "n/a"
		} else {
			log.Println("API limit info loaded from cache")
			// get caching time
			cachingTime := api.GetCachingTime()
			log.Println("Caching time:", cachingTime)

			// get time since last cache
			timeSinceLastCache := time.Since(cachingTime)
			log.Println("Time since last cache:", timeSinceLastCache)

		}

	}

	// create api limit info struct
	apiLimitsInfo := ApiLimitInfoJson{
		MaxRequestsPerMinute:   api.ApiInfo.MaxRequestsPerMinute,
		RemainingRequests:      api.ApiInfo.RemainingRequests,
		MaxRequestsPerDay:      api.ApiInfo.MaxRequestsPerDay,
		RemainingRequestsToday: api.ApiInfo.RemainingRequestsToday,
		CachingTime:            api.GetCachingTime(),
		TimeSinceLastCache:     time.Duration(time.Since(api.GetCachingTime()).Seconds()),
	}

	// convert api limit info struct to json
	apiLimitsInfoJson, err := json.Marshal(apiLimitsInfo)
	if err != nil {
		log.Println(err)
		return ""
	}

	return string(apiLimitsInfoJson)

}
