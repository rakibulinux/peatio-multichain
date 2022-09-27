package main

import (
	"context"
	"log"

	"github.com/zsmartex/multichain/chains/tron"
	"github.com/zsmartex/multichain/pkg/blockchain"
	"github.com/zsmartex/multichain/pkg/currency"
)

func main() {
	trxClient := tron.NewBlockchain()
	trxClient.Configure(&blockchain.Setting{
		URI: "http://demo.zsmartex.com:8090/",
		Currencies: []*currency.Currency{
			{
				ID:       "TRX",
				Subunits: 6,
			},
		},
	})

	n, err := trxClient.GetLatestBlockNumber(context.Background())
	if err != nil {

		log.Println(err)
	}

	log.Println(n)
}
