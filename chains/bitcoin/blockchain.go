package bitcoin

import (
	"encoding/json"
	"errors"
	"math/rand"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
	"github.com/volatiletech/null/v9"
	"github.com/zsmartex/multichain/pkg/block"
	"github.com/zsmartex/multichain/pkg/blockchain"
	"github.com/zsmartex/multichain/pkg/transaction"
)

type VOut struct {
	Value        decimal.Decimal `json:"value"`
	N            int64           `json:"n"`
	ScriptPubKey *struct {
		Addresses []string `json:"addresses"`
	}
}

type Vin struct {
	TxID string `json:"txid"`
	VOut int64  `json:"vout"`
}

type TxHash struct {
	TxID string  `json:"txid"`
	Vin  []*Vin  `json:"vin"`
	VOut []*VOut `json:"vout"`
}

type Block struct {
	Hash          string    `json:"hash"`
	Confirmations int       `json:"confirmations"`
	Size          int       `json:"size"`
	Height        int64     `json:"height"`
	Version       int       `json:"version"`
	MerkleRoot    string    `json:"merkleroot"`
	Tx            []*TxHash `json:"tx"`
}

type Blockchain struct {
	currency *blockchain.Currency
	settings *blockchain.Settings
	client   *resty.Client
}

func NewBlockchain() blockchain.Blockchain {
	return &Blockchain{
		client: resty.New(),
	}
}

func (b *Blockchain) Configure(settings *blockchain.Settings) error {
	b.settings = settings

	for _, c := range settings.Currencies {
		// allow only one currency
		b.currency = c
		break
	}

	return nil
}

func (b *Blockchain) jsonRPC(resp interface{}, method string, params ...interface{}) error {
	type Result struct {
		Version string           `json:"version"`
		ID      int              `json:"id"`
		Result  *json.RawMessage `json:"result"`
		Error   *json.RawMessage `json:"error"`
	}

	response, err := b.client.
		R().
		SetResult(Result{}).
		SetHeaders(map[string]string{
			"Accept":       "application/json",
			"Content-Type": "application/json",
		}).
		SetBody(map[string]interface{}{
			"version": "2.0",
			"id":      rand.Int(),
			"method":  method,
			"params":  params,
		}).Post(b.settings.URI)

	if err != nil {
		return err
	}

	result := response.Result().(*Result)

	if result.Error != nil {
		return errors.New("jsonRPC error: " + string(*result.Error))
	}

	if result.Result == nil {
		return errors.New("jsonRPC error: result is nil")
	}

	if err := json.Unmarshal(*result.Result, resp); err != nil {
		return err
	}

	return nil
}

func (b *Blockchain) GetLatestBlockNumber() (int64, error) {
	var resp int64
	if err := b.jsonRPC(&resp, "getblockcount"); err != nil {
		return 0, err
	}

	return resp, nil
}

func (b *Blockchain) GetBlockByNumber(block_number int64) (*block.Block, error) {
	var hash string
	if err := b.jsonRPC(&hash, "getblockhash", block_number); err != nil {
		return nil, err
	}

	return b.GetBlockByHash(hash)
}

func (b *Blockchain) GetBlockByHash(hash string) (*block.Block, error) {
	var resp *Block
	if err := b.jsonRPC(&resp, "getblock", hash, 2); err != nil {
		return nil, err
	}

	transactions := make([]*transaction.Transaction, 0)
	for _, tx := range resp.Tx {
		transactions = append(transactions, b.buildTransaction(tx)...)
	}

	return &block.Block{
		Hash:         resp.Hash,
		Number:       resp.Height,
		Transactions: transactions,
	}, nil
}

func (b *Blockchain) GetBalanceOfAddress(address string, _currency_id string) (decimal.Decimal, error) {
	var resp [][][]string
	if err := b.jsonRPC(&resp, "listaddressgroupings", address); err != nil {
		return decimal.Zero, err
	}

	for _, gr := range resp {
		for _, a := range gr {
			if a[0] == address {
				return decimal.NewFromString(a[1])
			}
		}
	}

	return decimal.Zero, errors.New("unavailable address balance")
}

func (b *Blockchain) GetTransaction(transaction_hash string) (txs []*transaction.Transaction, err error) {
	var resp *TxHash
	if err := b.jsonRPC(&resp, "getrawtransaction", transaction_hash, 1); err != nil {
		return nil, err
	}

	from_address, err := b.getFromAddress(resp)
	if err != nil {
		return nil, err
	}

	txs = make([]*transaction.Transaction, 0)
	for _, v := range b.buildVOut(resp.VOut) {
		fee, err := b.calculateFee(resp)
		if err != nil {
			return nil, err
		}

		txs = append(txs, &transaction.Transaction{
			TxHash:      null.StringFrom(resp.TxID),
			FromAddress: from_address,
			ToAddress:   v.ScriptPubKey.Addresses[0],
			Currency:    b.currency.ID,
			CurrencyFee: b.currency.ID,
			Fee:         fee,
			Amount:      v.Value,
			Status:      transaction.StatusSucceed,
		})
	}

	return
}

func (b *Blockchain) getFromAddress(tx *TxHash) (string, error) {
	var from_address string

	vin := tx.Vin[0]

	if len(vin.TxID) == 0 {
		return "", errors.New("unavailable from address")
	}

	var resp *TxHash
	if err := b.jsonRPC(&resp, "getrawtransaction", vin.TxID, 1); err != nil {
		return "", err
	}

	tx_src := resp.VOut[0]
	if len(tx_src.ScriptPubKey.Addresses) == 0 {
		return "", errors.New("unavailable from address")
	}

	from_address = tx_src.ScriptPubKey.Addresses[0]
	return from_address, nil
}

func (b *Blockchain) calculateFee(tx *TxHash) (decimal.Decimal, error) {
	vins := decimal.Zero
	vouts := decimal.Zero
	for _, v := range tx.Vin {
		vin := v.TxID
		vin_id := v.VOut

		if len(vin) == 0 {
			continue
		}

		var resp *TxHash
		if err := b.jsonRPC(&resp, "getrawtransaction", vin, 1); err != nil {
			return decimal.Zero, err
		}
		if len(resp.VOut) == 0 {
			continue
		}

		for _, vout := range resp.VOut {
			if vout.N != vin_id {
				continue
			}

			vins.Add(vout.Value)
		}
	}

	for _, vout := range tx.VOut {
		vouts.Add(vout.Value)
	}

	return vins.Sub(vouts), nil
}

func (b *Blockchain) buildVOut(vout []*VOut) []*VOut {
	nvout := make([]*VOut, 0)
	for _, v := range vout {
		if v.Value.IsPositive() && v.ScriptPubKey.Addresses != nil {
			nvout = append(nvout, v)
		}
	}

	return nvout
}

func (b *Blockchain) buildTransaction(tx *TxHash) []*transaction.Transaction {
	transactions := make([]*transaction.Transaction, 0)

	from_address, err := b.getFromAddress(tx)
	if err != nil {
		return transactions
	}

	for _, entry := range tx.VOut {
		if entry.Value.IsNegative() || entry.ScriptPubKey.Addresses == nil {
			continue
		}

		trans := &transaction.Transaction{
			Currency:    b.currency.ID,
			CurrencyFee: b.currency.ID,
			FromAddress: from_address,
			ToAddress:   entry.ScriptPubKey.Addresses[0],
			Amount:      entry.Value,
			TxHash:      null.StringFrom(tx.TxID),
			Status:      transaction.StatusSucceed,
		}

		transactions = append(transactions, trans)
	}

	return transactions
}
