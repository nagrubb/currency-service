package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type FreeCurrConvErrorResponse struct {
	Status int    `json:"status"`
	Error  string `json:"error"`
}

type ExchangeRate struct {
	From string
	To   string
	Rate float64
}

type ErrorResponse struct {
	Error string
}

type RestService struct {
	FreeCurrConvApiKey string
	RedisServer        string
	RedisCacheDuration time.Duration
}

var service *RestService

func main() {
	apiKeyFilename := os.Getenv("FREE_CURRCONV_API_KEY_FILE")
	redisServerAndPort := os.Getenv("REDIS_SERVER_AND_PORT")
	redisCacheDurationInMinutes := os.Getenv("REDIS_CACHE_DURATION_IN_MINUTES")
	apiKeyFile, err := os.Open(apiKeyFilename)
	defer apiKeyFile.Close()

	if err != nil {
		panic(err)
	}

	reader := bufio.NewReader(apiKeyFile)
	apiKey, moreToRead, err := reader.ReadLine()

	if err != nil {
		fmt.Println("Can't read file (%s) where api key should be stored", apiKeyFilename)
		panic(err)
	}

	if moreToRead {
		panic("API key is longer than expected")
	}

	minutes, err := strconv.ParseUint(redisCacheDurationInMinutes, 10, 32)

	if err != nil {
		fmt.Println(err)
		fmt.Println("Defaulting to caching for 15 minutes in Redis")
		minutes = 15
	}

	service = &RestService{
		FreeCurrConvApiKey: string(apiKey),
		RedisServer:        redisServerAndPort,
		RedisCacheDuration: time.Duration(minutes) * time.Minute,
	}

	fmt.Printf("%+v\n", service)
	service.startService()
}

func (rs RestService) startService() {
	router := mux.NewRouter()
	router.HandleFunc("/api/v1/currency/{from}/{to}", GetCurrency).Methods("GET")
	log.Fatal(http.ListenAndServe(":80", router))
}

func GetCurrency(responseWriter http.ResponseWriter, requestReader *http.Request) {
	params := mux.Vars(requestReader)

	//TODO: Sanitize
	fromCurrency := strings.ToUpper(params["from"])
	toCurrency := strings.ToUpper(params["to"])
	currencyQuery := fmt.Sprintf("%s_%s", fromCurrency, toCurrency)

	cachedValue, err := getCachedCurrencyValue(currencyQuery)
	if err == nil {
		fmt.Printf("Api=GetCurrency Action=UsingRedisCache Query=%s\n", currencyQuery)
		rate := &ExchangeRate{
			From: fromCurrency,
			To:   toCurrency,
			Rate: cachedValue,
		}

		if err := writeJson(responseWriter, rate); err != nil {
			responseWriter.WriteHeader(http.StatusInternalServerError)
			fmt.Println(err)
		}
		return
	}

	fmt.Printf("Api=GetCurrency Action=UsingCurrConv Query=%s\n", currencyQuery)
	url := url.URL{
		Scheme: "https",
		Host:   "free.currconv.com",
		Path:   "api/v7/convert",
	}

	queryString := url.Query()
	queryString.Set("q", currencyQuery)
	queryString.Set("compact", "ultra")
	queryString.Set("apiKey", service.FreeCurrConvApiKey)
	url.RawQuery = queryString.Encode()

	responseWriter.Header().Set("Content-Type", "application/json")

	rsp, err := http.Get(url.String())
	if err != nil {
		writeError(responseWriter, err.Error())
		return
	}

	defer rsp.Body.Close()
	rspBody, err := ioutil.ReadAll(rsp.Body)

	if err != nil {
		writeError(responseWriter, err.Error())
		return
	}

	//API returns either
	//200: {"<from_currency>_<to_currency>":<rate>}
	//400: {"status":<status_code>,"error":"error_message"}
	if rsp.StatusCode != http.StatusOK {
		var errorResponse FreeCurrConvErrorResponse

		if err = json.Unmarshal(rspBody, &errorResponse); err != nil {
			//Unknown response format
			writeError(responseWriter, err.Error())
			fmt.Println(strings.TrimSuffix(string(rspBody), "\n"))
			return
		}

		writeError(responseWriter, errorResponse.Error)
		return
	}

	var result map[string]float64
	if err = json.Unmarshal(rspBody, &result); err != nil {
		writeError(responseWriter, err.Error())
		fmt.Println(strings.TrimSuffix(string(rspBody), "\n"))
		return
	}

	if _, ok := result[currencyQuery]; !ok {
		writeError(responseWriter, fmt.Sprintf("%s not present in JSON result object", currencyQuery))
		return
	}

	var rate float64 = result[currencyQuery]

	if err := setCachedCurrencyValue(currencyQuery, rate); err != nil {
		fmt.Println(err)
	}

	rateResponse := &ExchangeRate{
		From: fromCurrency,
		To:   toCurrency,
		Rate: rate,
	}

	if err := writeJson(responseWriter, rateResponse); err != nil {
		responseWriter.WriteHeader(http.StatusInternalServerError)
		fmt.Println(err)
	}
}

func writeError(responseWriter http.ResponseWriter, errorString string) {
	responseWriter.WriteHeader(http.StatusInternalServerError)

	error := &ErrorResponse{
		Error: errorString,
	}

	if err := writeJson(responseWriter, error); err != nil {
		fmt.Println(err)
	}
}

func writeJson(responseWriter http.ResponseWriter, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	if _, err = responseWriter.Write(jsonData); err != nil {
		return err
	}

	return nil
}

func getCachedCurrencyValue(key string) (float64, error) {
	stringValue, err := getRedisServer().Get(context.Background(), key).Result()

	if err != nil {
		return 0, err
	}

	return strconv.ParseFloat(stringValue, 64)
}

func setCachedCurrencyValue(key string, value float64) error {
	_, err := getRedisServer().Set(context.Background(), key, value, service.RedisCacheDuration).Result()
	return err
}

func getRedisServer() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr: service.RedisServer,
	})
}
