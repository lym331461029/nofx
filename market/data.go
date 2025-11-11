package market

import (
	"encoding/json"
	"fmt"
	talib "github.com/markcheno/go-talib"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// Get 获取指定代币的市场数据
func Get(symbol string) (*Data, error) {
	var klines5m, klines4h []Kline
	var err error
	// 标准化symbol
	symbol = Normalize(symbol)
	// 获取5分钟K线数据 (最近10个)
	klines5m, err = WSMonitorCli.GetCurrentKlines(symbol, "5m") // 多获取一些用于计算
	if err != nil {
		return nil, fmt.Errorf("获取5分钟K线失败: %v", err)
	}

	klines30m, err := WSMonitorCli.GetCurrentKlines(symbol, "30m")
	if err != nil {
		return nil, fmt.Errorf("获取30分钟K线失败: %v", err)
	}

	// 获取4小时K线数据 (最近10个)
	klines4h, err = WSMonitorCli.GetCurrentKlines(symbol, "4h") // 多获取用于计算指标
	if err != nil {
		return nil, fmt.Errorf("获取4小时K线失败: %v", err)
	}

	// 计算当前指标 (基于5分钟最新数据)
	currentPrice := klines5m[len(klines5m)-1].Close
	currentEMA20 := calculateEMA(klines5m, 20)
	currentMACD := calculateMACD(klines5m)
	currentRSI7 := calculateRSI(klines5m, 7)

	// 计算价格变化百分比
	// 1小时价格变化 = 20个5分钟K线前的价格
	priceChange1h := 0.0
	if len(klines5m) >= 21 { // 至少需要21根K线 (当前 + 20根前)
		price1hAgo := klines5m[len(klines5m)-21].Close
		if price1hAgo > 0 {
			priceChange1h = ((currentPrice - price1hAgo) / price1hAgo) * 100
		}
	}

	// 4小时价格变化 = 1个4小时K线前的价格
	priceChange4h := 0.0
	if len(klines4h) >= 2 {
		price4hAgo := klines4h[len(klines4h)-2].Close
		if price4hAgo > 0 {
			priceChange4h = ((currentPrice - price4hAgo) / price4hAgo) * 100
		}
	}

	// 获取OI数据
	oiData, err := getOpenInterestData(symbol)
	if err != nil {
		// OI失败不影响整体,使用默认值
		oiData = &OIData{Latest: 0, Average: 0}
	}

	// 获取Funding Rate
	fundingRate, _ := getFundingRate(symbol)

	// 计算日内系列数据
	intradayData := calculateIntradaySeries(klines5m)

	middleTermData := calculateLongerTermData(klines30m)

	// 计算长期数据
	longerTermData := calculateLongerTermData(klines4h)

	return &Data{
		Symbol:            symbol,
		CurrentPrice:      currentPrice,
		PriceChange1h:     priceChange1h,
		PriceChange4h:     priceChange4h,
		CurrentEMA20:      currentEMA20,
		CurrentMACD:       currentMACD,
		CurrentRSI7:       currentRSI7,
		OpenInterest:      oiData,
		FundingRate:       fundingRate,
		IntradaySeries:    intradayData,
		MiddleTermContext: middleTermData,
		LongerTermContext: longerTermData,
	}, nil
}

// calculateEMA 计算EMA
func calculateEMA(klines []Kline, period int) float64 {
	if len(klines) < period {
		return 0
	}

	// 计算SMA作为初始EMA
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += klines[i].Close
	}
	ema := sum / float64(period)

	// 计算EMA
	multiplier := 2.0 / float64(period+1)
	for i := period; i < len(klines); i++ {
		ema = (klines[i].Close-ema)*multiplier + ema
	}

	return ema
}

// calculateMACD 计算MACD
func calculateMACD(klines []Kline) float64 {
	if len(klines) < 26 {
		return 0
	}

	// 计算12期和26期EMA
	ema12 := calculateEMA(klines, 12)
	ema26 := calculateEMA(klines, 26)

	// MACD = EMA12 - EMA26
	return ema12 - ema26
}

// calculateRSI 计算RSI
func calculateRSI(klines []Kline, period int) float64 {
	if len(klines) <= period {
		return 0
	}

	gains := 0.0
	losses := 0.0

	// 计算初始平均涨跌幅
	for i := 1; i <= period; i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			gains += change
		} else {
			losses += -change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	// 使用Wilder平滑方法计算后续RSI
	for i := period + 1; i < len(klines); i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			avgGain = (avgGain*float64(period-1) + change) / float64(period)
			avgLoss = (avgLoss * float64(period-1)) / float64(period)
		} else {
			avgGain = (avgGain * float64(period-1)) / float64(period)
			avgLoss = (avgLoss*float64(period-1) + (-change)) / float64(period)
		}
	}

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return rsi
}

// calculateATR 计算ATR
func calculateATR(klines []Kline, period int) float64 {
	if len(klines) <= period {
		return 0
	}

	trs := make([]float64, len(klines))
	for i := 1; i < len(klines); i++ {
		high := klines[i].High
		low := klines[i].Low
		prevClose := klines[i-1].Close

		tr1 := high - low
		tr2 := math.Abs(high - prevClose)
		tr3 := math.Abs(low - prevClose)

		trs[i] = math.Max(tr1, math.Max(tr2, tr3))
	}

	// 计算初始ATR
	sum := 0.0
	for i := 1; i <= period; i++ {
		sum += trs[i]
	}
	atr := sum / float64(period)

	// Wilder平滑
	for i := period + 1; i < len(klines); i++ {
		atr = (atr*float64(period-1) + trs[i]) / float64(period)
	}

	return atr
}

// calculateIntradaySeries 计算日内系列数据
func calculateIntradaySeries(klines []Kline) *IntradayData {
	data := &IntradayData{
		//MidPrices:        make([]float64, 0, 10),
		//EMA5Values:       make([]float64, 0, 10),
		//EMA20Values:      make([]float64, 0, 10),
		//MACDValues:       make([]float64, 0, 10),
		//MACDSignalValues: make([]float64, 0, 10),
		//MACDHistValues:   make([]float64, 0, 10),
		//RSI7Values:       make([]float64, 0, 10),
		//RSI14Values:      make([]float64, 0, 10),
	}

	for i := range klines {
		data.MidPrices = append(data.MidPrices, klines[i].Close)
	}

	// 计算EMA、MACD、RSI等指标
	data.MACDValues, data.MACDSignalValues, data.MACDHistValues = talib.Macd(data.MidPrices, 12, 26, 9)
	data.EMA5Values = talib.Ema(data.MidPrices, 5)
	data.EMA20Values = talib.Ema(data.MidPrices, 20)
	data.EMA50Values = talib.Ema(data.MidPrices, 50)
	data.RSI7Values = talib.Rsi(data.MidPrices, 7)
	data.RSI14Values = talib.Rsi(data.MidPrices, 14)
	data.Adx7Values = talib.Adx(data.MidPrices, data.MidPrices, data.MidPrices, 7)
	data.Adx14Values = talib.Adx(data.MidPrices, data.MidPrices, data.MidPrices, 14)

	// 获取最近10个数据点
	//start := len(klines) - 10
	//if start < 0 {
	//	start = 0
	//}

	//for i := start; i < len(klines); i++ {
	//	data.MidPrices = append(data.MidPrices, klines[i].Close)
	//	if i >= 4 {
	//		ema4 := calculateEMA(klines[:i+1], 4)
	//		data.EMA5Values = append(data.EMA5Values, ema4)
	//	}
	//
	//	// 计算每个点的EMA20
	//	if i >= 19 {
	//		ema20 := calculateEMA(klines[:i+1], 20)
	//		data.EMA20Values = append(data.EMA20Values, ema20)
	//	}
	//
	//	if i >= 49 {
	//		ema50 := calculateEMA(klines[:i+1], 50)
	//		data.EMA50Values = append(data.EMA50Values, ema50)
	//	}
	//
	//	// 计算每个点的MACD
	//	if i >= 25 {
	//		macd := calculateMACD(klines[:i+1])
	//		data.MACDValues = append(data.MACDValues, macd)
	//	}
	//
	//	// 计算每个点的RSI
	//	if i >= 7 {
	//		rsi7 := calculateRSI(klines[:i+1], 7)
	//		data.RSI7Values = append(data.RSI7Values, rsi7)
	//	}
	//	if i >= 14 {
	//		rsi14 := calculateRSI(klines[:i+1], 14)
	//		data.RSI14Values = append(data.RSI14Values, rsi14)
	//	}
	//}

	return data
}

// calculateLongerTermData 计算长期数据
func calculateLongerTermData(klines []Kline) *LongerTermData {
	data := &LongerTermData{
		MACDValues: make([]float64, 0, 10),
		//RSI14Values: make([]float64, 0, 10),
	}

	// 计算EMA
	data.EMA5 = calculateEMA(klines, 5)
	data.EMA20 = calculateEMA(klines, 20)
	data.EMA50 = calculateEMA(klines, 50)

	// 计算ATR
	data.ATR3 = calculateATR(klines, 3)
	data.ATR14 = calculateATR(klines, 14)

	// 计算成交量
	if len(klines) > 0 {
		data.CurrentVolume = klines[len(klines)-1].Volume
		// 计算平均成交量
		sum := 0.0
		for _, k := range klines {
			sum += k.Volume
		}
		data.AverageVolume = sum / float64(len(klines))
	}

	// 计算MACD和RSI序列
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	closed := make([]float64, len(klines))
	for i := range klines {
		closed[i] = klines[i].Close
	}
	data.MACDValues, data.MacdSignalValues, data.MACDHistValues = talib.Macd(closed, 12, 26, 9)
	data.RSI14Values = talib.Rsi(closed, 14)

	//for i := start; i < len(klines); i++ {
	//	if i >= 25 {
	//		macd := calculateMACD(klines[:i+1])
	//		data.MACDValues = append(data.MACDValues, macd)
	//	}
	//	if i >= 14 {
	//		rsi14 := calculateRSI(klines[:i+1], 14)
	//		data.RSI14Values = append(data.RSI14Values, rsi14)
	//	}
	//}

	return data
}

// getOpenInterestData 获取OI数据
func getOpenInterestData(symbol string) (*OIData, error) {
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/openInterest?symbol=%s", symbol)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		OpenInterest string `json:"openInterest"`
		Symbol       string `json:"symbol"`
		Time         int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	oi, _ := strconv.ParseFloat(result.OpenInterest, 64)

	return &OIData{
		Latest:  oi,
		Average: oi * 0.999, // 近似平均值
	}, nil
}

// getFundingRate 获取资金费率
func getFundingRate(symbol string) (float64, error) {
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/premiumIndex?symbol=%s", symbol)

	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result struct {
		Symbol          string `json:"symbol"`
		MarkPrice       string `json:"markPrice"`
		IndexPrice      string `json:"indexPrice"`
		LastFundingRate string `json:"lastFundingRate"`
		NextFundingTime int64  `json:"nextFundingTime"`
		InterestRate    string `json:"interestRate"`
		Time            int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	rate, _ := strconv.ParseFloat(result.LastFundingRate, 64)
	return rate, nil
}

// Format 格式化输出市场数据
func Format(data *Data) string {
	var sb strings.Builder

	//sb.WriteString(fmt.Sprintf("current_price = %.2f, current_macd = %.3f, current_rsi (7 period) = %.3f\n\n",
	//	data.CurrentPrice, data.CurrentMACD, data.CurrentRSI7))

	//sb.WriteString(fmt.Sprintf("In addition, here is the latest %s open interest and funding rate for perps:\n\n",
	//	data.Symbol))

	sb.WriteString(fmt.Sprintf("Here is the latest %s open interest and funding rate for perps:\n\n",
		data.Symbol))

	if data.OpenInterest != nil {
		sb.WriteString(fmt.Sprintf("Open Interest: Latest: %.2f Average: %.2f\n\n",
			data.OpenInterest.Latest, data.OpenInterest.Average))
	}

	sb.WriteString(fmt.Sprintf("Funding Rate: %.2e\n\n", data.FundingRate))

	if data.IntradaySeries != nil {
		sb.WriteString("Intraday series (5‑minute intervals, oldest → latest):\n\n")

		if len(data.IntradaySeries.MidPrices) > 0 {
			sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.IntradaySeries.MidPrices[len(data.IntradaySeries.MidPrices)-10:])))
		}

		if len(data.IntradaySeries.EMA5Values) > 0 {
			sb.WriteString(fmt.Sprintf("EMA indicators (5‑period): %s\n\n", formatFloatSlice(data.IntradaySeries.EMA5Values[len(data.IntradaySeries.EMA5Values)-10:])))
		}

		if len(data.IntradaySeries.EMA20Values) > 0 {
			sb.WriteString(fmt.Sprintf("EMA indicators (20‑period): %s\n\n", formatFloatSlice(data.IntradaySeries.EMA20Values[len(data.IntradaySeries.EMA20Values)-10:])))
		}

		if len(data.IntradaySeries.EMA50Values) > 0 {
			sb.WriteString(fmt.Sprintf("EMA indicators (50‑period): %s\n\n", formatFloatSlice(data.IntradaySeries.EMA50Values[len(data.IntradaySeries.EMA20Values)-10:])))
		}

		if len(data.IntradaySeries.MACDValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.IntradaySeries.MACDValues[len(data.IntradaySeries.EMA20Values)-10:])))
		}

		if len(data.IntradaySeries.MACDSignalValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD Signal indicators: %s\n\n", formatFloatSlice(data.IntradaySeries.MACDSignalValues[len(data.IntradaySeries.EMA20Values)-10:])))
		}

		if len(data.IntradaySeries.MACDHistValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD Histogram indicators: %s\n\n", formatFloatSlice(data.IntradaySeries.MACDHistValues[len(data.IntradaySeries.EMA20Values)-10:])))
		}

		if len(data.IntradaySeries.RSI7Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (7‑Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI7Values[len(data.IntradaySeries.EMA20Values)-10:])))
		}

		if len(data.IntradaySeries.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (14‑Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI14Values[len(data.IntradaySeries.EMA20Values)-10:])))
		}

		if len(data.IntradaySeries.Adx7Values) > 0 {
			sb.WriteString(fmt.Sprintf("ADX indicators (7‑Period): %s\n\n", formatFloatSlice(data.IntradaySeries.Adx7Values[len(data.IntradaySeries.EMA20Values)-10:])))
		}

		if len(data.IntradaySeries.Adx14Values) > 0 {
			sb.WriteString(fmt.Sprintf("ADX indicators (14‑Period): %s\n\n", formatFloatSlice(data.IntradaySeries.Adx14Values[len(data.IntradaySeries.EMA20Values)-10:])))
		}
	}

	if data.MiddleTermContext != nil {
		sb.WriteString("Medium‑term context (30m timeframe):\n\n")

		sb.WriteString(fmt.Sprintf("5‑Period EMA: %.3f vs. 20‑Period EMA: %.3f vs. 50‑Period EMA: %.3f\n\n",
			data.MiddleTermContext.EMA5, data.MiddleTermContext.EMA20, data.MiddleTermContext.EMA50))

		sb.WriteString(fmt.Sprintf("3‑Period ATR: %.3f vs. 14‑Period ATR: %.3f\n\n",
			data.MiddleTermContext.ATR3, data.MiddleTermContext.ATR14))

		sb.WriteString(fmt.Sprintf("Current Volume: %.3f vs. Average Volume: %.3f\n\n",
			data.MiddleTermContext.CurrentVolume, data.MiddleTermContext.AverageVolume))

		if len(data.MiddleTermContext.MACDValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.MiddleTermContext.MACDValues[len(data.MiddleTermContext.MACDValues)-10:])))
		}
		if len(data.MiddleTermContext.MacdSignalValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD Signal indicators: %s\n\n", formatFloatSlice(data.MiddleTermContext.MacdSignalValues[len(data.MiddleTermContext.MacdSignalValues)-10:])))
		}
		if len(data.MiddleTermContext.MACDHistValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD Histogram indicators: %s\n\n", formatFloatSlice(data.MiddleTermContext.MACDHistValues[len(data.MiddleTermContext.MACDHistValues)-10:])))
		}

		if len(data.MiddleTermContext.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (14‑Period): %s\n\n", formatFloatSlice(data.MiddleTermContext.RSI14Values[len(data.MiddleTermContext.RSI14Values)-10:])))
		}
	}

	//if data.LongerTermContext != nil {
	//	sb.WriteString("Longer‑term context (4‑hour timeframe):\n\n")
	//
	//	sb.WriteString(fmt.Sprintf("5‑Period EMA: %.3f vs. 20‑Period EMA: %.3f vs. 50‑Period EMA: %.3f\n\n",
	//		data.LongerTermContext.EMA5, data.LongerTermContext.EMA20, data.LongerTermContext.EMA50))
	//
	//	sb.WriteString(fmt.Sprintf("3‑Period ATR: %.3f vs. 14‑Period ATR: %.3f\n\n",
	//		data.LongerTermContext.ATR3, data.LongerTermContext.ATR14))
	//
	//	sb.WriteString(fmt.Sprintf("Current Volume: %.3f vs. Average Volume: %.3f\n\n",
	//		data.LongerTermContext.CurrentVolume, data.LongerTermContext.AverageVolume))
	//
	//	if len(data.LongerTermContext.MACDValues) > 0 {
	//		sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.LongerTermContext.MACDValues)))
	//	}
	//
	//	if len(data.LongerTermContext.RSI14Values) > 0 {
	//		sb.WriteString(fmt.Sprintf("RSI indicators (14‑Period): %s\n\n", formatFloatSlice(data.LongerTermContext.RSI14Values)))
	//	}
	//}

	return sb.String()
}

// formatFloatSlice 格式化float64切片为字符串
func formatFloatSlice(values []float64) string {
	strValues := make([]string, len(values))
	for i, v := range values {
		strValues[i] = fmt.Sprintf("%.3f", v)
	}
	return "[" + strings.Join(strValues, ", ") + "]"
}

// Normalize 标准化symbol,确保是USDT交易对
func Normalize(symbol string) string {
	symbol = strings.ToUpper(symbol)
	if strings.HasSuffix(symbol, "USDT") {
		return symbol
	}
	return symbol + "USDT"
}

// parseFloat 解析float值
func parseFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case string:
		return strconv.ParseFloat(val, 64)
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	default:
		return 0, fmt.Errorf("unsupported type: %T", v)
	}
}
