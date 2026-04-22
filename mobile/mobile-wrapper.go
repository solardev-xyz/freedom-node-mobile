package mobile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	beelite "github.com/Solar-Punk-Ltd/bee-lite"
	"github.com/ethersphere/bee/v2/pkg/api"
	"github.com/ethersphere/bee/v2/pkg/storage"
	"github.com/ethersphere/bee/v2/pkg/swarm"
)

const StringSliceDelimiter = "|"

type MobileNode interface {
	BlockchainData() (*BlockchainData, error)
	ConnectedPeerCount() int
	Download(hash string) (*FileDownloadResult, error)
	Shutdown() error
	WalletAddress() string
	FetchStamps()
	GetStampCount() int
	GetStamp(index int) *StampData
	BuyStamp(amountString string, depthString string, name string, immutable bool) (string, error)
	Upload(batchIdHex, filename, contentType string,
		act bool,
		historyAddressHex string,
		encrypt bool,
		rLevel byte,
		content []byte) (fileUploadResult *FileUploadResult, err error)
}

type MobileNodeImp struct {
	beeClient     *beelite.Beelite
	stampManager  *StampManager
	uploadManager *UploadManager
}

func StartNode(options *MobileNodeOptions, password string, verbosity string) (MobileNode, error) {

	beeliteOptions, err := convert(options)

	fmt.Printf("%+v\n", beeliteOptions)
	if err != nil {
		return nil, err
	}

	beeClient, err := beelite.Start(beeliteOptions, password, verbosity)
	if err != nil {
		return nil, err
	}

	return &MobileNodeImp{beeClient: beeClient, stampManager: NewStampManager(beeClient), uploadManager: NewUploadManager(beeClient)}, nil
}

func convert(options *MobileNodeOptions) (*beelite.LiteOptions, error) {
	validateErr := validate(options)
	if validateErr != nil {
		return nil, validateErr
	}

	bootNodes := []string{}
	if options.Bootnodes != "" {
		bootNodes = strings.Split(options.Bootnodes, StringSliceDelimiter)
	}

	staticNodesOpt := []string{}
	if options.StaticNodes != "" {
		staticNodesOpt = strings.Split(options.StaticNodes, StringSliceDelimiter)
	}

	return &beelite.LiteOptions{
		FullNodeMode:             options.FullNodeMode,
		BootnodeMode:             options.BootnodeMode,
		Bootnodes:                bootNodes,
		StaticNodes:              staticNodesOpt,
		DataDir:                  options.DataDir,
		WelcomeMessage:           options.WelcomeMessage,
		BlockchainRpcEndpoint:    options.BlockchainRpcEndpoint,
		SwapInitialDeposit:       options.SwapInitialDeposit,
		PaymentThreshold:         options.PaymentThreshold,
		SwapEnable:               options.SwapEnable,
		ChequebookEnable:         options.ChequebookEnable,
		UsePostageSnapshot:       options.UsePostageSnapshot,
		Mainnet:                  options.Mainnet,
		NetworkID:                uint64(options.NetworkID),
		NATAddr:                  options.NATAddr,
		CacheCapacity:            uint64(options.CacheCapacity),
		DBOpenFilesLimit:         uint64(options.DBOpenFilesLimit),
		DBWriteBufferSize:        uint64(options.DBWriteBufferSize),
		DBBlockCacheCapacity:     uint64(options.DBBlockCacheCapacity),
		DBDisableSeeksCompaction: options.DBDisableSeeksCompaction,
		RetrievalCaching:         options.RetrievalCaching,
	}, nil
}

func validate(options *MobileNodeOptions) error {
	if options.NetworkID < 0 {
		return errors.New("network ID must be a non-negative integer")
	}

	if options.CacheCapacity < 0 {
		return errors.New("cache capacity must be a non-negative integer")
	}

	if options.DBOpenFilesLimit < 0 {
		return errors.New("cache capacity must be a non-negative integer")
	}

	if options.DBWriteBufferSize < 0 {
		return errors.New("DBWriteBufferSize must be a non-negative integer")
	}

	if options.DBOpenFilesLimit < 0 {
		return errors.New("DBOpenFilesLimit must be a non-negative integer")
	}

	return nil
}

func (m *MobileNodeImp) Download(hash string) (*FileDownloadResult, error) {
	m.beeClient.GetLogger().Info("downloading: ", "hash", hash)

	var result *File = nil
	if hash == "" {
		e := fmt.Errorf("please enter a hash")
		return nil, e
	}
	dlAddr, err := swarm.ParseHexAddress(hash)
	if err != nil {
		return nil, err
	}

	ref, fileName, err := m.beeClient.GetBzz(context.Background(), dlAddr, nil, nil, nil)
	if err != nil {
		if errors.Is(err, beelite.ErrFailedToGetBzzReference) {
			return nil, nil
		}

		m.beeClient.GetLogger().Error(err, "download failed")
		return nil, err
	}

	hash = ""
	data, err := io.ReadAll(ref)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			m.beeClient.GetLogger().Info("content not found for hash: ", "hash", hash)
			return nil, nil
		}
		m.beeClient.GetLogger().Error(err, "convert to bytes failed")

		return nil, err
	}

	m.beeClient.GetLogger().Info("download succeeded", "fileName", fileName, "size", len(data))
	result = &File{Name: fileName, Data: data}

	return &FileDownloadResult{File: result, Stats: nil}, nil
}

func (m *MobileNodeImp) Upload(batchIdHex, filename, contentType string,
	act bool,
	historyAddressHex string,
	encrypt bool,
	redundancyLevel byte,
	content []byte) (fileUploadResult *FileUploadResult, err error) {

	historyAddress := swarm.MustParseHexAddress(historyAddressHex)
	reference, newHistoryAddress, err := m.uploadManager.Upload(batchIdHex, filename, contentType, act, historyAddress, encrypt, redundancyLevel, content)
	if err != nil {
		return nil, err
	}

	return &FileUploadResult{ReferenceHex: reference.String(), HistoryAddressHex: newHistoryAddress.String(), Stats: nil}, nil
}

// TODO remove later - ReactNative app still using it
func (m *MobileNodeImp) WalletAddress() string {
	return m.beeClient.OverlayEthAddress().String()
}

func (m *MobileNodeImp) BlockchainData() (*BlockchainData, error) {
	m.beeClient.GetLogger().Info("Getting blockchain data")

	chequebookBalance, err := m.getChequebookBalance()
	chequebookAddress := m.getChequebookAddr()

	if err != nil {
		m.beeClient.GetLogger().Error(err, "failed to get blockchain data")
		return nil, err
	}

	m.beeClient.GetLogger().Info("Blockchain data retrieved", "walletAddress", m.beeClient.OverlayEthAddress().String(), "chequebookAddress", chequebookAddress, "chequebookBalance", chequebookBalance)

	return &BlockchainData{
		WalletAddress:     m.beeClient.OverlayEthAddress().String(),
		ChequebookAddress: chequebookAddress,
		ChequebookBalance: chequebookBalance,
	}, nil
}

func (m *MobileNodeImp) ConnectedPeerCount() int {
	return m.beeClient.ConnectedPeerCount()
}

func (m *MobileNodeImp) Shutdown() error {
	err := m.beeClient.Shutdown()
	if err == nil {
		m.beeClient.GetLogger().Info("shutdown succeeded")
		return nil
	}
	m.beeClient.GetLogger().Error(err, "shutdown failed")
	return err
}

func (m *MobileNodeImp) getChequebookAddr() string {
	if m.beeClient.BeeNodeMode() == api.UltraLightMode {
		m.beeClient.GetLogger().Info("Node running in ultra-light mode, skipping getChequebookAddr query")
		return "N/A"
	}

	return m.beeClient.ChequebookAddr().String()
}

func (m *MobileNodeImp) getChequebookBalance() (string, error) {
	if m.beeClient.BeeNodeMode() == api.UltraLightMode {
		m.beeClient.GetLogger().Info("Node running in ultra-light mode, skipping getChequebookBalance query")
		return "N/A", nil
	}

	chequebookBalance, err := m.beeClient.ChequebookBalance()
	if err != nil {
		m.beeClient.GetLogger().Error(err, "failed to get chequebook balance")
		return "", err
	}

	return chequebookBalance.String(), nil
}

func (m *MobileNodeImp) FetchStamps() {
	m.stampManager.GetAllBatches()
}

func (m *MobileNodeImp) GetStampCount() int {
	return len(m.stampManager.stamps)
}

func (m *MobileNodeImp) GetStamp(index int) *StampData {
	if index < 0 || index >= len(m.stampManager.stamps) {
		return nil
	}
	return m.stampManager.stamps[index]
}

func (m *MobileNodeImp) BuyStamp(amountString string, depthString string, name string, immutable bool) (string, error) {
	return m.stampManager.BuyStamp(amountString, depthString, name, immutable)
}
