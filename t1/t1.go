package main

import (
	"context"
	"encoding/json"
	"github.com/adshao/go-binance/v2"
	"time"
)

var (
	apiKey    = "hWB2yqoNw4xTOU0uKSPlVZJfILgCzSLEK82cYsPmZdln06RKY09QpufiisR0P77C"
	secretKey = "0fAVqa4F8ilsxO00lpkD43vX4XjKEZ4WehgX2wkRdyDz3ZSXBncNkoyrtB0HwTSd"

	deepseekApiKey = "sk-e47daa8c2a46411b936857bf4e57f755"
)

func main() {

	//httpClient := http.Client{
	//	Timeout: 10 * 1e9,
	//}
	futuresClient := binance.NewFuturesClient(apiKey, secretKey) // USDT-M Futures
	klines, err := futuresClient.NewKlinesService().
		Interval("4h").
		StartTime(time.Now().Add(-10 * 24 * time.Hour).UnixMilli()).
		Symbol("BTCUSDT").Do(context.Background())
	if err != nil {
		panic(err)
	}

	klinesExt := FromRawKlines(klines)
	klinesExt.calcEma(5)
	klinesExt.calcEma(20)
	klinesExt.calcEma(60)
	klinesExt.calcMacd()
	klinesExt.calcRsi(7)
	klinesExt.calcRsi(14)

	data, _ := json.Marshal(klinesExt)
	println(string(data))
}
