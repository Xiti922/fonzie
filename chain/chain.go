package chain

import (
	"context"
	"fmt"
	"os"
	"strconv"

	cosmostypes "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	resty "github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
	lens "github.com/strangelove-ventures/lens/client"
	"github.com/umee-network/fonzie/customlens"
)

type Chains []*Chain

func (chains Chains) ImportMnemonic(ctx context.Context, mnemonic string) error {
	for _, info := range chains {
		err := info.ImportMnemonic(mnemonic)
		if err != nil {
			return err
		}
	}
	return nil
}

func (chains Chains) FindByPrefix(prefix string) *Chain {
	for _, info := range chains {
		if info.Prefix == prefix {
			return info
		}
	}
	return nil
}

type Chain struct {
	Prefix   string                        `json:"prefix"`
	RPC      string                        `json:"rpc"`
	CoinType uint32                        `json:"coin_type"`
	client   *customlens.CustomChainClient `json:"-"`
}

func (chain *Chain) getClient() *customlens.CustomChainClient {
	if chain.client == nil {
		if chain.CoinType == 0 {
			// default to cosmos
			chain.CoinType = 118
		}
		chainID, err := getChainID(chain.RPC)
		if err != nil {
			log.Fatalf("failed to get chain id for %s. err: %v", chain.Prefix, err)
		}
		// calculate gas adjustment from env
		gasAdjustment, err := strconv.ParseFloat(os.Getenv("GAS_ADJUSTMENT"), 64)
		if err != nil {
			gasAdjustment = 1.5
		}

		// Build chain config
		chainConfig := lens.ChainClientConfig{
			Key:            "anon",
			ChainID:        chainID,
			RPCAddr:        chain.RPC,
			AccountPrefix:  chain.Prefix,
			KeyringBackend: "memory",
			GasAdjustment:  gasAdjustment,
			Debug:          true,
			Timeout:        "5s",
			OutputFormat:   "json",
			SignModeStr:    "direct",
			Modules:        lens.ModuleBasics,
		}
		chainConfig.Key = "anon"

		// Creates client object to pull chain info
		c, err := lens.NewChainClient(&chainConfig, "", os.Stdin, os.Stdout)
		if err != nil {
			log.Fatal(err)
		}

		chain.client = &customlens.CustomChainClient{ChainClient: c}
	}
	return chain.client
}

func (chain *Chain) ImportMnemonic(mnemonic string) error {
	_, err := chain.getClient().KeyAddOrRestore("anon", chain.CoinType, mnemonic)
	if err != nil {
		return err
	}
	return nil
}

func (chain Chain) MultiSend(toAddr []cosmostypes.AccAddress, coins []cosmostypes.Coins, fees cosmostypes.Coins) error {
	c := chain.getClient()
	faucetRawAddr, err := c.GetKeyAddress()
	if err != nil {
		return err
	}
	faucetAddrStr, err := c.EncodeBech32AccAddr(faucetRawAddr)
	if err != nil {
		return err
	}

	var inputs []banktypes.Input
	var outputs []banktypes.Output
	for i := range toAddr {
		recipient, err := c.EncodeBech32AccAddr(toAddr[i])
		if err != nil {
			return err
		}
		log.Infof("Multi sending %s from faucet address [%s] to recipient [%s]",
			coins[i], faucetAddrStr, recipient)
		inputs = append(inputs, banktypes.Input{Address: faucetAddrStr, Coins: coins[i]})
		outputs = append(outputs, banktypes.Output{Address: recipient, Coins: coins[i]})
	}
	req := &banktypes.MsgMultiSend{
		Inputs:  inputs,
		Outputs: outputs,
	}

	return chain.sendMsg(req, fees, c)
}

func (chain Chain) DecodeAddr(a string) (cosmostypes.AccAddress, error) {
	c := chain.getClient()
	return c.DecodeBech32AccAddr(a)
}

func (chain Chain) Send(toAddr string, coins cosmostypes.Coins, fees cosmostypes.Coins) error {
	c := chain.getClient()
	faucetRawAddr, err := c.GetKeyAddress()
	if err != nil {
		return err
	}
	faucetAddr, err := c.EncodeBech32AccAddr(faucetRawAddr)
	if err != nil {
		return err
	}

	log.Infof("Sending %s from faucet address [%s] to recipient [%s]", coins, faucetAddr, toAddr)
	req := &banktypes.MsgSend{
		FromAddress: faucetAddr,
		ToAddress:   toAddr,
		Amount:      coins,
	}

	return chain.sendMsg(req, fees, c)
}

func (chain Chain) sendMsg(msg cosmostypes.Msg, fees cosmostypes.Coins, c *customlens.CustomChainClient) error {
	res, err := c.SendMsg(context.Background(), msg, fees.String())
	if err != nil {
		return err
	}
	fmt.Println(c.PrintTxResponse(res))
	return nil
}

func getChainID(rpcUrl string) (string, error) {
	rpc := resty.New().SetBaseURL(rpcUrl)

	resp, err := rpc.R().
		SetResult(map[string]interface{}{}).
		SetError(map[string]interface{}{}).
		Get("/commit")
	if err != nil {
		return "", err
	}

	if resp.IsError() {
		//return "", resp.Error().(*map[string]interface{})
		return "", fmt.Errorf("could not get chain id; http error code received %d", resp.StatusCode())
	}

	respbody := resp.Result().(*map[string]interface{})
	result := (*respbody)["result"]
	signedHeader := result.(map[string]interface{})["signed_header"]
	header := signedHeader.(map[string]interface{})["header"]
	chainID := header.(map[string]interface{})["chain_id"].(string)
	return chainID, nil
}

/*
"result": {
	"signed_header": {
	  "header": {
	    "version": {
	      "block": "11"
	    },
	    "chain_id": "umee-1",
	    "height": "731426",
*/
