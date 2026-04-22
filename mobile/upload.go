package mobile

import (
	"bytes"
	"context"
	"io"

	beelite "github.com/Solar-Punk-Ltd/bee-lite"
	"github.com/ethersphere/bee/v2/pkg/file/redundancy"
	"github.com/ethersphere/bee/v2/pkg/swarm"
)

type UploadManager struct {
	beeClient *beelite.Beelite
}

func NewUploadManager(beeClient *beelite.Beelite) *UploadManager {
	return &UploadManager{beeClient: beeClient}
}

func (um *UploadManager) Upload(batchIdHex, filename, contentType string,
	act bool,
	historyAddress swarm.Address,
	encrypt bool,
	rLevel byte,
	reader []byte) (reference swarm.Address, newHistoryAddress swarm.Address, err error) {

	byteReader := bytes.NewReader(reader)
	redundancyLevel := getRedundancyLevel(rLevel)
	return um.beeClient.AddFileBzz(context.Background(), batchIdHex, filename, contentType, act, historyAddress, encrypt, redundancyLevel, io.Reader(byteReader))
}

func getRedundancyLevel(rLevel byte) redundancy.Level {
	switch rLevel {
	case 1:
		return redundancy.MEDIUM
	case 2:
		return redundancy.STRONG
	case 3:
		return redundancy.INSANE
	case 4:
		return redundancy.PARANOID
	default:
		return redundancy.NONE
	}
}
