package mobile

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"

	beelite "github.com/Solar-Punk-Ltd/bee-lite"
)

type StampManager struct {
	stamps    []*StampData
	beeClient *beelite.Beelite
}

func NewStampManager(beeClient *beelite.Beelite) *StampManager {
	return &StampManager{
		stamps:    []*StampData{},
		beeClient: beeClient,
	}
}

func (sm *StampManager) GetAllBatches() {
	batches := sm.beeClient.GetAllBatches()

	stamps := make([]*StampData, len(batches))
	for i, batch := range batches {
		stamps[i] = &StampData{
			Label:         batch.Label(),
			BatchIdHex:    hex.EncodeToString(batch.ID()),
			BatchAmount:   batch.Amount().String(),
			BatchDepth:    batch.Depth(),
			BucketDepth:   batch.BucketDepth(),
			ImmutableFlag: batch.ImmutableFlag(),
		}
	}

	sm.stamps = stamps
}

func (sm *StampManager) BuyStamp(amountString string, depthString string, name string, immutable bool) (string, error) {
	amount := new(big.Int)
	if _, ok := amount.SetString(amountString, 10); !ok {
		return "", fmt.Errorf("invalid amount string: %s", amountString)
	}

	depth, err := parseUint64(depthString)
	if err != nil {
		return "", fmt.Errorf("invalid depth string: %s", depthString)
	}

	hash, _, err := sm.beeClient.BuyStamp(amount, depth, name, immutable)
	if err != nil {
		return "", err
	}

	return hash.String(), nil
}

func parseUint64(s string) (uint64, error) {
	return strconv.ParseUint(s, 10, 64)
}
