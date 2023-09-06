package tron

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/fbsobreira/gotron-sdk/pkg/address"
	"github.com/fbsobreira/gotron-sdk/pkg/client"
	"github.com/fbsobreira/gotron-sdk/pkg/common"
	"github.com/fbsobreira/gotron-sdk/pkg/proto/api"
	"github.com/fbsobreira/gotron-sdk/pkg/proto/core"
	"github.com/shopspring/decimal"
	"github.com/volatiletech/null/v9"
	"github.com/zsmartex/multichain/chains/tron/concerns"
	"github.com/zsmartex/multichain/pkg/block"
	"github.com/zsmartex/multichain/pkg/blockchain"
	"github.com/zsmartex/multichain/pkg/currency"
	"github.com/zsmartex/multichain/pkg/transaction"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type Blockchain struct {
	currency     *currency.Currency
	contracts    []*currency.Currency
	currencies   []*currency.Currency
	client       *client.GrpcClient
	walletClient api.WalletClient
	setting      *blockchain.Setting
}

func NewBlockchain() blockchain.Blockchain {
	return &Blockchain{
		contracts: make([]*currency.Currency, 0),
	}
}

func (b *Blockchain) Configure(setting *blockchain.Setting) {
	b.setting = setting
	b.currencies = setting.Currencies

	if setting != nil {
		if len(setting.URI) > 0 {
			b.client = client.NewGrpcClientWithTimeout(setting.URI, 5*time.Second)
			b.client.Start(grpc.WithInsecure())
			b.walletClient = b.client.Client
		}
	}

	for _, c := range setting.Currencies {
		if c.Options["trc20_contract_address"] != nil {
			b.contracts = append(b.contracts, c)
		} else {
			b.currency = c
		}
	}
}

func (b *Blockchain) GetLatestBlockNumber(ctx context.Context) (int64, error) {
	block, err := b.walletClient.GetNowBlock(ctx, new(api.EmptyMessage))
	if err != nil {
		return 0, err
	}

	return block.BlockHeader.RawData.Number, nil
}

func (b *Blockchain) GetBlockByNumber(ctx context.Context, blockNumber int64) (*block.Block, error) {
	maxSizeOption := grpc.MaxCallRecvMsgSize(32 * 10e6)

	block, err := b.walletClient.GetBlockByNum(ctx, &api.NumberMessage{
		Num: blockNumber,
	}, maxSizeOption)
	if err != nil {
		return nil, err
	}

	return b.buildBlock(ctx, block)
}

func (b *Blockchain) GetBlockByHash(ctx context.Context, hash string) (*block.Block, error) {
	blockID := new(api.BytesMessage)
	var err error

	blockID.Value, err = common.FromHex(hash)
	if err != nil {
		return nil, fmt.Errorf("get block by id: %v", err)
	}

	maxSizeOption := grpc.MaxCallRecvMsgSize(32 * 10e6)
	block, err := b.walletClient.GetBlockById(ctx, blockID, maxSizeOption)
	if err != nil {
		return nil, err
	}

	return b.buildBlock(ctx, block)
}

func (b *Blockchain) buildBlock(ctx context.Context, blk *core.Block) (*block.Block, error) {
	transactions := make([]*transaction.Transaction, 0)
	for _, t := range blk.Transactions {
		trans, err := b.buildTransaction(ctx, t)
		if err != nil {
			return nil, err
		}

		for _, t2 := range trans {
			t2.BlockNumber = blk.BlockHeader.RawData.Number
		}

		transactions = append(transactions, trans...)
	}

	return &block.Block{
		Number:       blk.BlockHeader.RawData.Number,
		Transactions: transactions,
	}, nil
}

func (b *Blockchain) buildTransaction(ctx context.Context, tx *core.Transaction) ([]*transaction.Transaction, error) {
	var err error
	if len(tx.RawData.Contract) == 0 {
		if b.invalidTxn(tx) {
			return nil, errors.New("transaction contract not found")
		}
	}

	if tx.RawData.Contract[0].Type == core.Transaction_Contract_TransferContract || tx.RawData.Contract[0].Type == core.Transaction_Contract_TransferAssetContract {
		if b.invalidTxn(tx) {
			return nil, errors.New("transaction invalid txn")
		}

		if tx.RawData.Contract[0].Type == core.Transaction_Contract_TransferContract {
			txs, err := b.buildTrxTransaction(tx)
			if err != nil {
				return nil, err
			}

			return txs, nil
		}
	}

	txID, err := concerns.TransactionToHex(tx)
	if err != nil {
		return nil, err
	}

	transactionID := new(api.BytesMessage)
	transactionID.Value, err = common.FromHex(txID)
	if err != nil {
		return nil, fmt.Errorf("get transaction by id error: %v", err)
	}

	maxSizeOption := grpc.MaxCallRecvMsgSize(32 * 10e6)
	txInfo, err := b.walletClient.GetTransactionInfoById(ctx, transactionID, maxSizeOption)
	if err != nil {
		return nil, err
	}

	if !bytes.Equal(transactionID.Value, txInfo.Id) {
		return nil, fmt.Errorf("transaction info not found")
	}

	if b.invalidTrc20Txn(txInfo) {
		return nil, errors.New("transaction invalid trc20 txn")
	}

	if len(txInfo.ContractAddress) == 0 {
		return []*transaction.Transaction{}, nil
	}

	return b.buildTrc20Transaction(tx, txInfo)
}

func (b *Blockchain) invalidTxn(tx *core.Transaction) bool {
	txContract := tx.GetRawData().Contract[0]
	var transferContract core.TransferContract
	if err := anypb.UnmarshalTo(txContract.GetParameter(), &transferContract, proto.UnmarshalOptions{}); err != nil {
		return true
	}

	return transferContract.Amount == 0 || tx.Ret[0].ContractRet == core.Transaction_Result_REVERT
}

func (b *Blockchain) invalidTrc20Txn(txn *core.TransactionInfo) bool {
	if txn.Log == nil {
		return false
	}

	return len(txn.ContractAddress) == 0 || len(txn.Log) == 0
}

func (b *Blockchain) buildTrxTransaction(tx *core.Transaction) ([]*transaction.Transaction, error) {
	transactions := make([]*transaction.Transaction, 0)

	for _, txContract := range tx.GetRawData().Contract {
		var transferContract core.TransferContract
		if err := anypb.UnmarshalTo(txContract.GetParameter(), &transferContract, proto.UnmarshalOptions{}); err != nil {
			return nil, err
		}

		fromAddress := address.Address(transferContract.OwnerAddress)
		toAddress := address.Address(transferContract.ToAddress)

		txHash, err := concerns.TransactionToHex(tx)
		if err != nil {
			return nil, err
		}

		transactions = append(transactions, &transaction.Transaction{
			Currency:    b.currency.ID,
			CurrencyFee: b.currency.ID,
			TxHash:      null.StringFrom(txHash),
			ToAddress:   toAddress.String(),
			FromAddress: fromAddress.String(),
			Amount:      decimal.NewFromBigInt(big.NewInt(transferContract.Amount), -b.currency.Subunits),
			Fee:         decimal.NewNullDecimal(decimal.NewFromFloat(1)),
			Status:      transaction.StatusSucceed,
		})
	}

	return transactions, nil
}

func (b *Blockchain) buildTrc20Transaction(tx *core.Transaction, txnReceipt *core.TransactionInfo) ([]*transaction.Transaction, error) {
	transactions := make([]*transaction.Transaction, 0)

	if b.trc20TxnStatus(txnReceipt) == transaction.StatusFailed && len(txnReceipt.Log) == 0 {
		return b.buildInvalidTrc20Txn(txnReceipt)
	}

	for _, txTriggerSmartContract := range tx.GetRawData().Contract {
		var transferTriggerSmartContract core.TriggerSmartContract
		err := proto.Unmarshal(txTriggerSmartContract.Parameter.GetValue(), &transferTriggerSmartContract)
		if err != nil {
			return nil, err
		}

		for _, log := range txnReceipt.Log {
			if len(log.Topics) == 0 || common.Bytes2Hex(log.Topics[0]) != "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef" {
				continue
			}

			var c *currency.Currency
			for _, contract := range b.contracts {
				contractAddress := address.Address(transferTriggerSmartContract.ContractAddress)
				if strings.EqualFold(contract.Options["trc20_contract_address"].(string), contractAddress.String()) {
					c = contract
					break
				}
			}

			if c == nil {
				continue
			}

			bigAmount := new(big.Int).SetBytes(log.Data)

			fromAddress := address.HexToAddress(fmt.Sprintf("41%s", common.Bytes2Hex(log.Topics[1])[24:]))
			toAddress := address.HexToAddress(fmt.Sprintf("41%s", common.Bytes2Hex(log.Topics[2])[24:]))
			amount := decimal.NewFromBigInt(bigAmount, -c.Subunits)

			transactions = append(transactions, &transaction.Transaction{
				Currency:    c.ID,
				CurrencyFee: b.currency.ID,
				TxHash:      null.StringFrom(common.Bytes2Hex(txnReceipt.GetId())),
				ToAddress:   toAddress.String(),
				FromAddress: fromAddress.String(),
				Amount:      amount,
				Fee:         decimal.NewNullDecimal(decimal.NewFromFloat(10)),
				Status:      b.trc20TxnStatus(txnReceipt),
			})
		}
	}

	return transactions, nil
}

func (b *Blockchain) trc20TxnStatus(txnReceipt *core.TransactionInfo) transaction.Status {
	if txnReceipt.Receipt.Result == core.Transaction_Result_SUCCESS {
		return transaction.StatusSucceed
	} else {
		return transaction.StatusFailed
	}
}

func (b *Blockchain) buildInvalidTrc20Txn(txnReceipt *core.TransactionInfo) ([]*transaction.Transaction, error) {
	var c *currency.Currency
	for _, contract := range b.contracts {
		contractAddress := address.Address(txnReceipt.ContractAddress)

		if strings.EqualFold(contract.Options["trc20_contract_address"].(string), contractAddress.String()) {
			c = contract
			break
		}
	}

	if c == nil {
		return nil, nil
	}

	return []*transaction.Transaction{
		{
			Currency:    c.ID,
			CurrencyFee: b.currency.ID,
			TxHash:      null.StringFrom(common.Bytes2Hex(txnReceipt.GetId())),
			Status:      b.trc20TxnStatus(txnReceipt),
		},
	}, nil
}

func (b *Blockchain) GetBalanceOfAddress(ctx context.Context, address string, currencyID string) (decimal.Decimal, error) {
	var c *currency.Currency
	for _, cu := range b.currencies {
		if cu.ID == currencyID {
			c = cu
			break
		}
	}

	if c == nil {
		return decimal.Zero, errors.New("currency not found")
	}

	if c.Options["trc20_contract_address"] != nil {
		return b.loadTrc20Balance(ctx, address, c)
	} else {
		return b.loadTrxBalance(ctx, address)
	}
}

func (b *Blockchain) loadTrxBalance(ctx context.Context, address string) (decimal.Decimal, error) {
	result, err := b.client.GetAccount(address)
	if err != nil {
		return decimal.Zero, nil
	}

	return decimal.NewFromBigInt(big.NewInt(result.Balance), -b.currency.Subunits), nil
}

func (b *Blockchain) loadTrc20Balance(ctx context.Context, address string, currency *currency.Currency) (decimal.Decimal, error) {
	big, err := b.client.TRC20ContractBalance(address, currency.Options["trc20_contract_address"].(string))
	if err != nil {
		return decimal.Zero, nil
	}

	return decimal.NewFromBigInt(big, -b.currency.Subunits), nil
}

func (b *Blockchain) GetTransaction(ctx context.Context, transactionHash string) ([]*transaction.Transaction, error) {
	var err error
	transactionID := new(api.BytesMessage)
	transactionID.Value, err = common.FromHex(transactionHash)
	if err != nil {
		return nil, fmt.Errorf("get transaction by id error: %v", err)
	}

	maxSizeOption := grpc.MaxCallRecvMsgSize(32 * 10e6)
	tx, err := b.walletClient.GetTransactionById(ctx, transactionID, maxSizeOption)
	if err != nil {
		return nil, err
	}

	if size := proto.Size(tx); size == 0 {
		return nil, fmt.Errorf("transaction info not found")
	}

	ts, err := b.buildTransaction(ctx, tx)
	if err != nil {
		return nil, err
	}

	return ts, err
}
