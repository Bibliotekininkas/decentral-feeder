package scrapers

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	models "github.com/diadata-org/decentral-feeder/pkg/models"
	"github.com/diadata-org/decentral-feeder/pkg/utils"
	ws "github.com/gorilla/websocket"
)

// A krakenWSSubscribeMessage represents a message to subscribe the public/private channel.
type krakenWSSubscribeMessage struct {
	Method string       `json:"method"`
	Params krakenParams `json:"params"`
}

type krakenParams struct {
	Channel string   `json:"channel"`
	Symbol  []string `json:"symbol"`
}

type krakenWSResponse struct {
	Channel string                 `json:"channel"`
	Type    string                 `json:"type"`
	Data    []krakenWSResponseData `json:"data"`
}

type krakenWSResponseData struct {
	Symbol    string  `json:"symbol"`
	Side      string  `json:"side"`
	Price     float64 `json:"price"`
	Size      float64 `json:"qty"`
	OrderType string  `json:"order_type"`
	TradeID   int     `json:"trade_id"`
	Time      string  `json:"timestamp"`
}

type krakenScraper struct {
	wsClient         *ws.Conn
	tradesChannel    chan models.Trade
	subscribeChannel chan models.ExchangePair
	tickerPairMap    map[string]models.Pair
	lastTradeTimeMap map[string]time.Time
	maxErrCount      int
	restartWaitTime  int
	genesis          time.Time
}

var (
	krakenWSBaseString = "wss://ws.kraken.com/v2"
)

func NewKrakenScraper(ctx context.Context, pairs []models.ExchangePair, failoverChannel chan string, wg *sync.WaitGroup) Scraper {
	defer wg.Done()
	var lock sync.RWMutex
	log.Info("Started Kraken scraper.")

	scraper := krakenScraper{
		tradesChannel:    make(chan models.Trade),
		subscribeChannel: make(chan models.ExchangePair),
		tickerPairMap:    models.MakeTickerPairMap(pairs),
		lastTradeTimeMap: make(map[string]time.Time),
		maxErrCount:      20,
		restartWaitTime:  5,
		genesis:          time.Now(),
	}

	var wsDialer ws.Dialer
	wsClient, _, err := wsDialer.Dial(krakenWSBaseString, nil)
	if err != nil {
		log.Error("Dial Kraken ws base string: ", err)
		failoverChannel <- string(KRAKEN_EXCHANGE)
		return &scraper
	}
	scraper.wsClient = wsClient

	// Subscribe to pairs.
	for _, pair := range pairs {
		if err := scraper.subscribe(pair, true, &lock); err != nil {
			log.Errorf("Kraken - subscribe to pair %s: %v", pair.ForeignName, err)
		} else {
			log.Infof("Kraken - Subscribed to pair %s:%s", KRAKEN_EXCHANGE, pair.ForeignName)
			scraper.lastTradeTimeMap[pair.ForeignName] = time.Now()
		}

	}

	go scraper.fetchTrades()

	// Check last trade time for each subscribed pair and resubscribe if no activity for more than @krakenWatchdogDelayMap.
	for _, pair := range pairs {
		envVar := strings.ToUpper(KRAKEN_EXCHANGE) + "_WATCHDOG_" + strings.Split(strings.ToUpper(pair.ForeignName), "-")[0] + "_" + strings.Split(strings.ToUpper(pair.ForeignName), "-")[1]
		watchdogDelay, err := strconv.ParseInt(utils.Getenv(envVar, "60"), 10, 64)
		if err != nil {
			log.Error("Parse krakenWatchdogDelay: ", err)
		}
		watchdogTicker := time.NewTicker(time.Duration(watchdogDelay) * time.Second)
		go watchdog(ctx, pair, watchdogTicker, scraper.lastTradeTimeMap, watchdogDelay, scraper.subscribeChannel, &lock)
		go scraper.resubscribe(ctx, &lock)
	}

	return &scraper

}

func (scraper *krakenScraper) Close(cancel context.CancelFunc) error {
	log.Warn("Kraken - call scraper.Close()")
	cancel()
	return scraper.wsClient.Close()
}

func (scraper *krakenScraper) TradesChannel() chan models.Trade {
	return scraper.tradesChannel
}

func (scraper *krakenScraper) fetchTrades() {
	// Read trades stream.
	var errCount int
	for {

		var message krakenWSResponse
		err := scraper.wsClient.ReadJSON(&message)
		if err != nil {
			if handleErrorReadJSON(err, &errCount, scraper.maxErrCount, scraper.restartWaitTime) {
				return
			}
			continue
		}

		if message.Channel == "trade" {
			for _, data := range message.Data {

				// Parse trade quantities.
				price, volume, timestamp, foreignTradeID, err := parseKrakenTradeMessage(data)
				if err != nil {
					log.Error("Kraken - parseTradeMessage: ", err)
				}

				// Identify ticker symbols with underlying assets.
				pair := strings.Split(data.Symbol, "/")
				var exchangepair models.Pair
				if len(pair) > 1 {
					exchangepair = scraper.tickerPairMap[pair[0]+pair[1]]
				}

				trade := models.Trade{
					QuoteToken:     exchangepair.QuoteToken,
					BaseToken:      exchangepair.BaseToken,
					Price:          price,
					Volume:         volume,
					Time:           timestamp,
					Exchange:       models.Exchange{Name: KRAKEN_EXCHANGE},
					ForeignTradeID: foreignTradeID,
				}
				// log.Info("Got trade: ", trade)

				scraper.tradesChannel <- trade
			}
		}
	}
}

func (scraper *krakenScraper) resubscribe(ctx context.Context, lock *sync.RWMutex) {
	for {
		select {
		case pair := <-scraper.subscribeChannel:
			err := scraper.subscribe(pair, false, lock)
			if err != nil {
				log.Errorf("Kraken - Unsubscribe pair %s: %v", pair.ForeignName, err)
			} else {
				log.Infof("Kraken - Unsubscribed pair %s:%s", KRAKEN_EXCHANGE, pair.ForeignName)
			}
			time.Sleep(2 * time.Second)
			err = scraper.subscribe(pair, true, lock)
			if err != nil {
				log.Errorf("Kraken - Resubscribe pair %s: %v", pair.ForeignName, err)
			} else {
				log.Infof("Kraken - Subscribed to pair %s:%s", KRAKEN_EXCHANGE, pair.ForeignName)
			}
		case <-ctx.Done():
			log.Warn("-------------------------------------Kraken - close resubscribe routine of scraper with genesis: ", scraper.genesis)
			return
		}
	}
}

func (scraper *krakenScraper) subscribe(pair models.ExchangePair, subscribe bool, lock *sync.RWMutex) error {
	defer lock.Unlock()
	subscribeType := "unsubscribe"
	if subscribe {
		subscribeType = "subscribe"
	}
	a := &krakenWSSubscribeMessage{
		Method: subscribeType,
		Params: krakenParams{
			Channel: "trade",
			Symbol:  []string{pair.UnderlyingPair.QuoteToken.Symbol + "/" + pair.UnderlyingPair.BaseToken.Symbol},
		},
	}
	log.Infof("Subscribed for Pair %s:%s", KRAKEN_EXCHANGE, pair.ForeignName)
	lock.Lock()
	return scraper.wsClient.WriteJSON(a)
}

func (scraper *krakenScraper) unsubscribe(pair models.ExchangePair, lock *sync.RWMutex) error {
	defer lock.Unlock()
	a := &krakenWSSubscribeMessage{
		Method: "unsubscribe",
		Params: krakenParams{
			Channel: "trade",
			Symbol:  []string{pair.UnderlyingPair.QuoteToken.Symbol + "/" + pair.UnderlyingPair.BaseToken.Symbol},
		},
	}
	log.Infof("Unsubscribed for Pair %s:%s", KRAKEN_EXCHANGE, pair.ForeignName)
	lock.Lock()
	return scraper.wsClient.WriteJSON(a)
}

func parseKrakenTradeMessage(message krakenWSResponseData) (price float64, volume float64, timestamp time.Time, foreignTradeID string, err error) {
	price = message.Price
	volume = message.Size
	if message.Side == "sell" {
		volume -= 1
	}
	timestamp, err = time.Parse("2006-01-02T15:04:05.000000Z", message.Time)
	if err != nil {
		return
	}

	foreignTradeID = strconv.Itoa(message.TradeID)
	return
}
