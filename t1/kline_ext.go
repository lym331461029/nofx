package main

import (
	"github.com/adshao/go-binance/v2/futures"
	"github.com/markcheno/go-talib"
	"reflect"
	"strconv"
)

type KlineExt struct {
	futures.Kline
	EMA5       float64
	EMA20      float64
	EMA60      float64
	Macd       float64
	MacdSignal float64
	MacdHist   float64
	Rsi7       float64
	Rsi14      float64
}

func FromRawKline(rawKline *futures.Kline) *KlineExt {
	return &KlineExt{
		Kline: *rawKline,
	}
}

func FromRawKlines(rawKlines []*futures.Kline) KLinesExtSlice {
	klines := make([]*KlineExt, 0)
	for _, rawKline := range rawKlines {
		klines = append(klines, FromRawKline(rawKline))
	}
	return klines
}

type KLinesExtSlice []*KlineExt

func (kes *KLinesExtSlice) ClosePrices() []float64 {
	return kes.FieldSlice("Close")
}

func (kes *KLinesExtSlice) OpenPrices() []float64 {
	return kes.FieldSlice("Open")
}

func (kes *KLinesExtSlice) FieldSlice(filedName string) []float64 {
	// 通过反射获取指定字段的值,如果类型不是float64则转换
	values := make([]float64, 0)
	for _, kline := range *kes {
		value := reflect.ValueOf(kline).Elem().FieldByName(filedName)
		if value.IsValid() {
			if value.Kind() == reflect.Float64 {
				values = append(values, value.Float())
			} else if value.Kind() == reflect.String {
				v, _ := strconv.ParseFloat(value.String(), 64)
				values = append(values, v)
			}
		}
	}
	return values
}

func (kes *KLinesExtSlice) setFieldSlice(filedName string, data []float64) {
	for i, kline := range *kes {
		value := reflect.ValueOf(kline).Elem().FieldByName(filedName)
		if value.IsValid() && value.CanSet() && value.Kind() == reflect.Float64 {
			value.SetFloat(data[i])
		}
	}
}

func (kes *KLinesExtSlice) calcEma(period int) {
	values := talib.Ema(kes.ClosePrices(), period)
	fieldName := "EMA" + strconv.Itoa(period)
	kes.setFieldSlice(fieldName, values)
}

func (kes *KLinesExtSlice) calcMacd() {
	macd, signal, hist := talib.Macd(kes.ClosePrices(), 12, 26, 9)
	kes.setFieldSlice("Macd", macd)
	kes.setFieldSlice("MacdSignal", signal)
	kes.setFieldSlice("MacdHist", hist)
}

func (kes *KLinesExtSlice) calcRsi(period int) {
	values := talib.Rsi(kes.ClosePrices(), period)
	fieldName := "Rsi" + strconv.Itoa(period)
	kes.setFieldSlice(fieldName, values)
}
