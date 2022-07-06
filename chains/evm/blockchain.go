package evm

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/shopspring/decimal"
	"github.com/volatiletech/null/v9"
	"github.com/zsmartex/multichain/pkg/block"
	"github.com/zsmartex/multichain/pkg/blockchain"
	"github.com/zsmartex/multichain/pkg/transaction"
)

var abiDefinition = `[{"constant":true,"inputs":[],"name":"name","outputs":[{"name":"","type":"string"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"name":"_spender","type":"address"},{"name":"_value","type":"uint256"}],"name":"approve","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"totalSupply","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"name":"_from","type":"address"},{"name":"_to","type":"address"},{"name":"_value","type":"uint256"}],"name":"transferFrom","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[],"name":"decimals","outputs":[{"name":"","type":"uint8"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[{"name":"_owner","type":"address"}],"name":"balanceOf","outputs":[{"name":"balance","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":true,"inputs":[],"name":"symbol","outputs":[{"name":"","type":"string"}],"payable":false,"stateMutability":"view","type":"function"},{"constant":false,"inputs":[{"name":"_to","type":"address"},{"name":"_value","type":"uint256"}],"name":"transfer","outputs":[{"name":"","type":"bool"}],"payable":false,"stateMutability":"nonpayable","type":"function"},{"constant":true,"inputs":[{"name":"_owner","type":"address"},{"name":"_spender","type":"address"}],"name":"allowance","outputs":[{"name":"","type":"uint256"}],"payable":false,"stateMutability":"view","type":"function"},{"payable":true,"stateMutability":"payable","type":"fallback"},{"anonymous":false,"inputs":[{"indexed":true,"name":"owner","type":"address"},{"indexed":true,"name":"spender","type":"address"},{"indexed":false,"name":"value","type":"uint256"}],"name":"Approval","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"name":"from","type":"address"},{"indexed":true,"name":"to","type":"address"},{"indexed":false,"name":"value","type":"uint256"}],"name":"Transfer","type":"event"}]`
var tokenEventIdentifier = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

type Blockchain struct {
	currency  *blockchain.Currency
	contracts []*blockchain.Currency
	client    *ethclient.Client
	settings  *blockchain.Settings
}

func NewBlockchain() blockchain.Blockchain {
	return &Blockchain{
		contracts: make([]*blockchain.Currency, 0),
	}
}

func (b *Blockchain) Configure(settings *blockchain.Settings) error {
	rpc_client, err := rpc.Dial(settings.URI)
	if err != nil {
		return err
	}

	client := ethclient.NewClient(rpc_client)
	b.client = client
	b.settings = settings

	for _, c := range settings.Currencies {
		if c.Options["erc20_contract_address"] != nil {
			b.contracts = append(b.contracts, c)
		} else {
			b.currency = c
		}
	}

	return nil
}

func (b *Blockchain) GetLatestBlockNumber() (int64, error) {
	block_number, err := b.client.BlockNumber(context.Background())

	return int64(block_number), err
}

func (b *Blockchain) GetBlockByNumber(block_number int64) (*block.Block, error) {
	result, err := b.client.BlockByNumber(context.Background(), big.NewInt(block_number))
	if err != nil {
		return nil, err
	}

	return b.GetBlockByHash(result.Hash().Hex())
}

func (b *Blockchain) GetBlockByHash(hash string) (*block.Block, error) {
	result, err := b.client.BlockByHash(context.Background(), common.HexToHash(hash))
	if err != nil {
		return nil, err
	}

	transactions := make([]*transaction.Transaction, 0)
	for _, t := range result.Transactions() {
		txs, err := b.buildTransaction(t)
		if err != nil {
			return nil, err
		}

		transactions = append(transactions, txs...)
	}

	return &block.Block{
		Hash:         result.Hash().Hex(),
		Number:       result.Number().Int64(),
		Transactions: transactions,
	}, nil
}

func (b *Blockchain) GetTransaction(txHash string) ([]*transaction.Transaction, error) {
	result, _, err := b.client.TransactionByHash(context.Background(), common.HexToHash(txHash))
	if err != nil {
		return nil, err
	}

	ts, err := b.buildTransaction(result)
	if err != nil {
		return nil, err
	}

	return ts, nil
}

func (b *Blockchain) GetBalanceOfAddress(address string, currency_id string) (decimal.Decimal, error) {
	for _, contract := range b.contracts {
		if currency_id == contract.ID {
			return b.getERC20Balance(address, contract)
		}
	}

	block_number, err := b.GetLatestBlockNumber()
	if err != nil {
		return decimal.Zero, err
	}

	amount, err := b.client.BalanceAt(context.Background(), common.HexToAddress(address), big.NewInt(block_number))
	if err != nil {
		return decimal.Zero, err
	}

	return decimal.NewFromBigInt(amount, -b.currency.SubUnits), nil
}

func (b *Blockchain) getERC20Balance(address string, currency *blockchain.Currency) (decimal.Decimal, error) {
	contract_address := common.HexToAddress(currency.Options["erc20_contract_address"].(string))

	block_number, err := b.GetLatestBlockNumber()
	if err != nil {
		return decimal.Zero, err
	}

	abi, err := abi.JSON(strings.NewReader(abiDefinition))
	if err != nil {
		return decimal.Zero, err
	}

	data, err := abi.Pack("balanceOf", common.HexToAddress(address))
	if err != nil {
		return decimal.Zero, err
	}

	bytes, err := b.client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &contract_address,
		Data: data,
	}, big.NewInt(block_number))
	if err != nil {
		return decimal.Zero, err
	}

	return decimal.NewFromBigInt(new(big.Int).SetBytes(bytes), -currency.SubUnits), nil
}

func (b *Blockchain) buildTransaction(tx *types.Transaction) ([]*transaction.Transaction, error) {
	receipt, err := b.client.TransactionReceipt(context.Background(), tx.Hash())
	if err != nil {
		return nil, err
	}

	if len(receipt.Logs) > 0 {
		return b.buildERC20Transactions(tx, receipt)
	} else {
		return b.buildETHTransactions(tx, receipt)
	}
}

func (b *Blockchain) buildETHTransactions(tx *types.Transaction, receipt *types.Receipt) ([]*transaction.Transaction, error) {
	var to_address string

	from_address, err := types.LatestSignerForChainID(tx.ChainId()).Sender(tx)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	cost := decimal.NewFromBigInt(tx.Cost(), -b.currency.SubUnits)
	amount := decimal.NewFromBigInt(tx.Value(), -b.currency.SubUnits)
	fee := cost.Sub(amount)
	to := tx.To()

	if to != nil {
		to_address = to.Hex()
	}

	return []*transaction.Transaction{
		{
			Currency:    b.currency.ID,
			CurrencyFee: b.currency.ID,
			TxHash:      null.StringFrom(tx.Hash().Hex()),
			FromAddress: from_address.Hex(),
			ToAddress:   to_address,
			Fee:         fee,
			Amount:      amount,
			Status:      b.transactionStatus(receipt),
		},
	}, nil
}

func (b *Blockchain) buildERC20Transactions(tx *types.Transaction, receipt *types.Receipt) ([]*transaction.Transaction, error) {
	if b.transactionStatus(receipt) == transaction.StatusFailed && len(receipt.Logs) == 0 {
		return b.buildInvalidErc20Transaction(tx, receipt)
	}

	fee := decimal.NewFromBigInt(big.NewInt(int64(receipt.GasUsed*tx.GasFeeCap().Uint64())), -b.currency.SubUnits)

	transactions := make([]*transaction.Transaction, 0)
	for _, l := range receipt.Logs {
		if len(l.BlockHash.Bytes()) == 0 && l.BlockNumber == 0 {
			continue
		}

		if len(l.Topics) == 0 || l.Topics[0].Hex() != tokenEventIdentifier {
			continue
		}

		// Contract: l.Address.Hex()
		fromAddress := fmt.Sprintf("0x%s", l.Topics[1].Hex()[26:])
		toAddress := fmt.Sprintf("0x%s", l.Topics[2].Hex()[26:])

		i := new(big.Int)
		i.SetString(common.Bytes2Hex(l.Data), 16)
		value := decimal.NewFromBigInt(i, -6)

		for _, c := range b.contracts {
			if strings.EqualFold(c.Options["erc20_contract_address"].(string), l.Address.Hex()) {
				transactions = append(transactions, &transaction.Transaction{
					Currency:    c.ID,
					CurrencyFee: b.currency.ID,
					TxHash:      null.StringFrom(tx.Hash().Hex()),
					FromAddress: fromAddress,
					ToAddress:   toAddress,
					Fee:         fee,
					Amount:      value,
					Status:      b.transactionStatus(receipt),
				})
			}
		}
	}

	return transactions, nil
}

func (b *Blockchain) buildInvalidErc20Transaction(tx *types.Transaction, receipt *types.Receipt) ([]*transaction.Transaction, error) {
	fee := decimal.NewFromBigInt(big.NewInt(int64(receipt.GasUsed*tx.GasFeeCap().Uint64())), -b.currency.SubUnits)

	transactions := make([]*transaction.Transaction, 0)

	for _, c := range b.contracts {
		if c.Options["erc20_contract_address"] == tx.To().Hex() {
			transactions = append(transactions, &transaction.Transaction{
				TxHash:      null.StringFrom(tx.Hash().Hex()),
				BlockNumber: receipt.BlockNumber.Int64(),
				CurrencyFee: b.currency.ID,
				Currency:    c.ID,
				Fee:         fee,
				Status:      b.transactionStatus(receipt),
			})
		}
	}

	return transactions, nil
}

func (b *Blockchain) transactionStatus(receiptTx *types.Receipt) transaction.Status {
	switch receiptTx.Status {
	case 1:
		return transaction.StatusSucceed
	case 0:
		return transaction.StatusFailed
	default:
		return transaction.StatusPending
	}
}
