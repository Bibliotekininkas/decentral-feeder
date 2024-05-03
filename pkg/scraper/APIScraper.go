package scrapers

import (
	"sync"

	models "github.com/diadata-org/diaprotocol/pkg/models"
)

// RunScraper returns an API scraper for @exchange. If scrape==true it actually does
// scraping. Otherwise can be used for pairdiscovery.
func RunScraper(exchange string, pairs []string, tradesChannel chan models.Trade, wg *sync.WaitGroup) {
	switch exchange {
	case BINANCE_EXCHANGE:
		NewBinanceScraper(Exchanges[exchange].Name, pairs, tradesChannel, wg)
	}
}
