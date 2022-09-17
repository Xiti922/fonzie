package main

import (
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/cosmos/cosmos-sdk/types"
	log "github.com/sirupsen/logrus"

	"github.com/umee-network/fonzie/chain"
)

/*
 * 1. create a worker -> a go routine which will consume a channel
 * 2. the worker will wait for new requests and have a time guard for processing faucet requests
 *   - we will batch requests in 4s, max 50 requests per transactions
 *
 */

type FaucetReq struct {
	Recipient types.AccAddress
	Coins     types.Coins
	Fees      types.Coins
	session   *discordgo.Session
	msg       *discordgo.MessageCreate
}

type ChainFaucet struct {
	channel chan FaucetReq
	chain   *chain.Chain
}

func (cf ChainFaucet) Consume(quit chan bool) {
	log.Info("starting worker ", cf.chain.Prefix)
	var r FaucetReq
	var rs []FaucetReq
	const interval = time.Second * 7
	var t = time.NewTicker(interval)

	for {
		select {
		case r = <-cf.channel:
			log.Infof("%s worker NEW request, req: %v", cf.chain.Prefix, r)
			rs = append(rs, r)
			if len(rs) > 160 {
				cf.processRequests(rs)
				rs = make([]FaucetReq, 0)
				if isDebug {
					log.Infof("DEBUG: %s worker processed request, req: %v", cf.chain.Prefix, r)
				}
				t.Reset(interval)
			} else {
				log.Infof("%s worker waiting for more requests, %v", cf.chain.Prefix, r)
			}
		case <-t.C:
			log.Infof("%s worker ticker, #num req: %d", cf.chain.Prefix, len(rs))
			if len(rs) > 0 {
				cf.processRequests(rs)
				rs = make([]FaucetReq, 0)
			}

		case <-quit:
			if len(rs) > 0 {
				cf.processRequests(rs)
			}
			// die so kubernetes restarts pod
			log.Fatal("Worker ", cf.chain.Prefix, " quit")
		}
	}
}

func (cf ChainFaucet) processRequests(rs []FaucetReq) {
	var toAddrss = make([]types.AccAddress, 0, len(rs))
	var coins = make([]types.Coins, 0, len(rs))
	var fees = make(types.Coins, 0, len(rs))
	for _, r := range rs {
		toAddrss = append(toAddrss, r.Recipient)
		coins = append(coins, r.Coins)
		fees = fees.Add(r.Fees...)
	}
	err := cf.chain.MultiSend(toAddrss, coins, fees)
	if err != nil {
		for _, r := range rs {
			reportError(r.session, r.msg, err)
		}
	} else {
		for _, r := range rs {
			if isDebug {
				log.Infof("DEBUG: %s worker processed request, req: %v", cf.chain.Prefix, r)
			}
			// Everything worked, so-- respond successfully to Discord requester
			if (r.session != nil) && (r.msg != nil) {
				sendReaction(r.session, r.msg, "✅")
				sendMessage(r.session, r.msg, fmt.Sprintf("Dispensed 💸 `%s` to `%s`", r.Coins, r.Recipient))
			}
		}
	}
}
