package diverclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/VigorousDeveloper/poc-human/app"
	"github.com/cosmos/cosmos-sdk/client"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/hashicorp/go-retryablehttp"
	rpchttp "github.com/tendermint/tendermint/rpc/client/http"
)

type (
	// TxID is a string that can uniquely represent a transaction on different
	// block chain
	TxID string
	// TxIDs is a slice of TxID
	TxIDs []TxID
)

// BlankTxID represent blank
var BlankTxID = TxID("0000000000000000000000000000000000000000000000000000000000000000")

// NewTxID parse the input hash as TxID
func NewTxID(hash string) (TxID, error) {
	switch len(hash) {
	case 64:
		// do nothing
	case 66: // ETH check
		if !strings.HasPrefix(hash, "0x") {
			err := fmt.Errorf("txid error: must be 66 characters (got %d)", len(hash))
			return TxID(""), err
		}
	default:
		err := fmt.Errorf("txid error: must be 64 characters (got %d)", len(hash))
		return TxID(""), err
	}

	return TxID(strings.ToUpper(hash)), nil
}

type BridgeConfig struct {
	ChainId         string
	ChainHost       string
	ChainRPC        string
	ChainHomeFolder string
}

// DiversifiChainBridge will be used to send tx to DIVERSIChain
type DiversifiChainBridge struct {
	keys          *Keys
	cfg           BridgeConfig
	blockHeight   uint64
	accountNumber uint64
	seqNumber     uint64
	httpClient    *retryablehttp.Client
	broadcastLock *sync.RWMutex

	signerName                  string
	lastBlockHeightCheck        time.Time
	lastDiversichainBlockHeight uint64
	pubKey                      string
	voterAddress                string
}

// NewDiversifiChainBridge create a new instance of DiversifiChainBridge
func NewDiversifiChainBridge(k *Keys, cfg *BridgeConfig, signer string, pubKey string, voter string) (*DiversifiChainBridge, error) {
	httpClient := retryablehttp.NewClient()
	httpClient.Logger = nil

	return &DiversifiChainBridge{
		keys:          k,
		httpClient:    httpClient,
		signerName:    signer,
		broadcastLock: &sync.RWMutex{},
		pubKey:        pubKey,
		voterAddress:  voter,
		cfg:           *cfg,
	}, nil
}

// GetContext return a valid context with all relevant values set
func (b *DiversifiChainBridge) GetContext() client.Context {
	ctx := client.Context{}
	ctx = ctx.WithKeyring(b.keys.GetKeybase())
	ctx = ctx.WithChainID(b.cfg.ChainId)
	ctx = ctx.WithHomeDir(b.cfg.ChainHomeFolder)
	ctx = ctx.WithFromName(b.signerName)
	ctx = ctx.WithFromAddress(b.keys.GetSignerInfo().GetAddress())
	ctx = ctx.WithBroadcastMode("sync")

	encodingConfig := app.MakeEncodingConfig()
	ctx = ctx.WithCodec(encodingConfig.Marshaler)
	ctx = ctx.WithInterfaceRegistry(encodingConfig.InterfaceRegistry)
	ctx = ctx.WithTxConfig(encodingConfig.TxConfig)
	ctx = ctx.WithLegacyAmino(encodingConfig.Amino)
	ctx = ctx.WithAccountRetriever(authtypes.AccountRetriever{})

	remote := b.cfg.ChainRPC
	if !strings.HasSuffix(b.cfg.ChainHost, "http") {
		remote = fmt.Sprintf("tcp://%s", remote)
	}
	ctx = ctx.WithNodeURI(remote)
	client, err := rpchttp.New(remote, "/websocket")
	if err != nil {
		panic(err)
	}
	ctx = ctx.WithClient(client)
	return ctx
}

func (b *DiversifiChainBridge) getWithPath(path string) ([]byte, int, error) {
	return b.get(b.getDiversifiChainURL(path))
}

// getThorChainURL with the given path
func (b *DiversifiChainBridge) getDiversifiChainURL(path string) string {
	uri := url.URL{
		Scheme: "http",
		Host:   b.cfg.ChainHost,
		Path:   path,
	}
	return uri.String()
}

// get handle all the low level http GET calls using retryablehttp.ThorchainBridge
func (b *DiversifiChainBridge) get(url string) ([]byte, int, error) {
	resp, err := b.httpClient.Get(url)
	if err != nil {
		fmt.Println("ffailed to GET from diversifichain")
		return nil, http.StatusNotFound, fmt.Errorf("failed to GET from diversifichain: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Println("failed to close response body")
		}
	}()

	buf, err := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return buf, resp.StatusCode, errors.New("Status code: " + resp.Status + " returned")
	}
	if err != nil {
		fmt.Println("fail_read_diversifihain_resp", "")
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}
	return buf, resp.StatusCode, nil
}

//
func (b *DiversifiChainBridge) GetVoterInfo() (string, string) {
	return b.pubKey, b.voterAddress
}

//
func (b *DiversifiChainBridge) GetMonikerName() string {
	return b.signerName
}

// getAccountNumberAndSequenceNumber returns account and Sequence number required to post into thorchain
func (b *DiversifiChainBridge) getAccountNumberAndSequenceNumber() (uint64, uint64, error) {
	path := fmt.Sprintf("%s/%s", "/cosmos/auth/v1beta1/accounts", b.keys.GetSignerInfo().GetAddress())

	body, _, err := b.getWithPath(path)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get auth accounts: %w", err)
	}

	var resp AccountResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, 0, fmt.Errorf("failed to unmarshal account resp: %w", err)
	}

	acc := resp.Account
	return acc.AccountNumber, acc.Sequence, nil
}

// DiversifiBlockTime Block time of DIVERSIChain
var DiversifiBlockTime = 5 * time.Second

// GetBlockHeight returns the current height for diversifi blocks
func (b *DiversifiChainBridge) GetBlockHeight() (uint64, error) {
	if time.Since(b.lastBlockHeightCheck) < DiversifiBlockTime && b.lastDiversichainBlockHeight > 0 {
		return b.lastDiversichainBlockHeight, nil
	}

	latestBlock, err := b.GetLastBlock("")
	if err != nil {
		return 0, fmt.Errorf("failed to GetDiverchainHeight: %w", err)
	}

	b.lastBlockHeightCheck = time.Now()
	h, _ := strconv.ParseUint(latestBlock.Block.Header.Height, 10, 64)

	return h, nil
}

// getLastBlock calls the /lastblock/{chain} endpoint and Unmarshal's into the QueryResLastBlockHeights type
func (b *DiversifiChainBridge) GetLastBlock(chain string) (QueryResLastBlockHeights, error) {
	path := "/cosmos/base/tendermint/v1beta1/blocks/latest"
	if chain != "" {
		path = fmt.Sprintf("%s/%s", path, chain)
	}
	buf, _, err := b.getWithPath(path)
	if err != nil {
		return QueryResLastBlockHeights{}, fmt.Errorf("failed to get lastblock: %w", err)
	}

	var lastBlock QueryResLastBlockHeights
	if err := json.Unmarshal(buf, &lastBlock); err != nil {
		return QueryResLastBlockHeights{}, fmt.Errorf("failed to unmarshal last block: %w", err)
	}
	return lastBlock, nil
}

// Get Transaction Data List
func (b *DiversifiChainBridge) GetTxDataList(chain string) (QueryTransactionDataList, error) {
	path := "/Diversifi-Technologies/diversifi/diversifi/transaction_data"
	if chain != "" {
		path = fmt.Sprintf("%s/%s", path, chain)
	}
	buf, _, err := b.getWithPath(path)
	if err != nil {
		return QueryTransactionDataList{}, fmt.Errorf("failed to get lastblock: %w", err)
	}

	var txDataList QueryTransactionDataList
	if err := json.Unmarshal(buf, &txDataList); err != nil {
		return QueryTransactionDataList{}, fmt.Errorf("failed to unmarshal last block: %w", err)
	}
	return txDataList, nil
}
