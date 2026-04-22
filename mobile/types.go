package mobile

type MobileNodeOptions struct {
	FullNodeMode             bool
	BootnodeMode             bool
	Bootnodes                string
	StaticNodes              string
	DataDir                  string
	WelcomeMessage           string
	BlockchainRpcEndpoint    string
	SwapInitialDeposit       string
	PaymentThreshold         string
	SwapEnable               bool
	ChequebookEnable         bool
	UsePostageSnapshot       bool
	Mainnet                  bool
	NetworkID                int64
	NATAddr                  string
	CacheCapacity            int64
	DBOpenFilesLimit         int64
	DBWriteBufferSize        int64
	DBBlockCacheCapacity     int64
	DBDisableSeeksCompaction bool
	RetrievalCaching         bool
}

type File struct {
	Name string
	Data []byte
}

type FileDownloadResult struct {
	File  *File
	Stats *ReadableBandwidthStats
}

type BlockchainData struct {
	WalletAddress     string
	ChequebookAddress string
	ChequebookBalance string
}

type StampData struct {
	Label         string
	BatchIdHex    string
	BatchAmount   string
	BatchDepth    byte
	BucketDepth   byte
	ImmutableFlag bool
}

type FileUploadResult struct {
	ReferenceHex      string
	HistoryAddressHex string
	Stats             *ReadableBandwidthStats
}

type ReadableBandwidthStats struct {
	TotalInMB   string
	TotalOutMB  string
	RateInMBps  string
	RateOutMBps string
}
