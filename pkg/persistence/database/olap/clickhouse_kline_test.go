package olap

import (
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/shopspring/decimal"
)

type Qu_Trade struct {
	entity.Model
	Price         decimal.Decimal `json:"price"`           // 成交价格
	Amount        decimal.Decimal `json:"amount"`          // 成交数量
	BuyUserId     string          `json:"buy_user_id"`     // 买方用户ID
	SellUserId    string          `json:"sell_user_id"`    // 卖方用户ID
	BuyerOrderId  uint            `json:"buyer_order_id"`  // 买方订单ID
	SellerOrderId uint            `json:"seller_order_id"` // 卖方订单ID
	TradeTime     int64           `json:"trade_time"`      // 成交时间
	IsBuyerMaker  bool            `json:"is_buyer_maker"`  // 买方是否为挂单方
	IsBestMatch   bool            `json:"is_best_match"`   // 是否为最优撮合单
	IndexPrice    decimal.Decimal `json:"index_price"`     // 指数价格
	MarkPrice     decimal.Decimal `json:"mark_price"`      // 标记价格
	OpenInterest  decimal.Decimal `json:"open_interest"`   // Open interest at the time of trade
}

type Qu_Klines struct {
	entity.Model
	PriceType          int             `json:"priceType"`          // 价格类型（最新价格、指数价格、标记价格）
	PriceChange        decimal.Decimal `json:"priceChange"`        // 价格变动
	PriceChangePercent decimal.Decimal `json:"priceChangePercent"` // 价格变动百分比
	WeightedAvgPrice   decimal.Decimal `json:"weightedAvgPrice"`   // 加权平均价
	LastPrice          decimal.Decimal `json:"lastPrice"`          // 最新价
	LastQty            decimal.Decimal `json:"lastQty"`            // 最新成交量
	OpenPrice          decimal.Decimal `json:"openPrice"`          // 开盘价
	HighPrice          decimal.Decimal `json:"highPrice"`          // 最高价
	LowPrice           decimal.Decimal `json:"lowPrice"`           // 最低价
	ClosePrice         decimal.Decimal `json:"closePrice"`         // 收盘价
	Volume             decimal.Decimal `json:"volume"`             // 成交量
	QuoteVolume        decimal.Decimal `json:"quoteVolume"`        // 成交额
	OpenTime           int64           `json:"openTime"`           // 开盘时间
	CloseTime          int64           `json:"closeTime"`          // 收盘时间
	FirstId            int64           `json:"firstId"`            // 首笔成交ID
	LastId             int64           `json:"lastId"`             // 末笔成交ID
	Count              int64           `json:"count"`              // 成交笔数
	BuyerVolume        decimal.Decimal `json:"buyer_volume"`       // 主动买入成交量
	BuyerQuote         decimal.Decimal `json:"buyer_quote"`        // 主动买入成交额
	Interval           string          `json:"interval"`           // 时间间隔
}
