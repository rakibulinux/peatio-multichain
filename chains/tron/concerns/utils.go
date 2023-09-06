package concerns

import (
	"crypto/sha256"
	"fmt"

	"github.com/fbsobreira/gotron-sdk/pkg/common"
	"github.com/fbsobreira/gotron-sdk/pkg/proto/core"
	"google.golang.org/protobuf/proto"
)

func TransactionToHex(tx *core.Transaction) (string, error) {
	rawData, err := proto.Marshal(tx.GetRawData())
	if err != nil {
		return "", fmt.Errorf("proto marshal tx raw data error: %v", err)
	}

	h256h := sha256.New()
	h256h.Write(rawData)
	hash := h256h.Sum(nil)

	return common.BytesToHash(hash).String()[2:], nil
}
