package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
  "bufio"
	"strings"
)

type ExchangeRate struct {
	From string
	To   string
	Rate float64
}

type RestService struct {
	FreeCurrConvApiKey string
}

var service *RestService

func main() {
	apiKeyFilename := os.Getenv("FREE_CURRCONV_API_KEY_FILE")
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

	service = &RestService{ FreeCurrConvApiKey: string(apiKey) }
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

	rsp, err := http.Get(url.String())
	if err != nil {
		responseWriter.WriteHeader(http.StatusInternalServerError)
		fmt.Println(err)
		return
	}

	defer rsp.Body.Close()
	rspBody, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		responseWriter.WriteHeader(http.StatusInternalServerError)
		fmt.Println(err)
		return
	}

	var result map[string]float64
	if err = json.Unmarshal(rspBody, &result); err != nil {
		responseWriter.WriteHeader(http.StatusInternalServerError)
		fmt.Println(err)
		fmt.Print(strings.TrimSuffix(string(rspBody), "\n"))
		return
	}

	if _, ok := result[currencyQuery]; !ok {
		responseWriter.WriteHeader(http.StatusInternalServerError)
		fmt.Println(fmt.Sprintf("%s not present in JSON result object", currencyQuery))
		return
	}

	rate := &ExchangeRate{
		From: fromCurrency,
		To:   toCurrency,
		Rate: result[currencyQuery],
	}

	jsonData, err := json.Marshal(rate)
	if err != nil {
		responseWriter.WriteHeader(http.StatusInternalServerError)
		fmt.Println(err)
		return
	}

	responseWriter.Header().Set("Content-Type", "application/json")
	if _, err = responseWriter.Write(jsonData); err != nil {
		responseWriter.WriteHeader(http.StatusInternalServerError)
		fmt.Println(err)
		return
	}
}
