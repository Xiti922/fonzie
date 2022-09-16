package db

import (
	"context"
	"crypto/md5"
	"os"
	"time"

	b64 "encoding/base64"
	"encoding/hex"

	"cloud.google.com/go/firestore"
	firebase "firebase.google.com/go"
	cosmostypes "github.com/cosmos/cosmos-sdk/types"
	log "github.com/sirupsen/logrus"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ChainPrefix = string
type Username = string
type FundingReceipt struct {
	ChainPrefix ChainPrefix       `firestore:"chainPrefix"`
	Username    Username          `firestore:"username"`
	FundedAt    time.Time         `firestore:"fundedAt"`
	Amount      cosmostypes.Coins `firestore:"amount"`
}
type FundingReceipts []FundingReceipt

// Db represents the application interface for accessing the database
type Db struct {
	firestore *firestore.Client
}

func NewDb(ctx context.Context) Db {
	// Initialize Firestore
	client, err := initFirestore(ctx)
	if err != nil {
		log.Fatal(err)
	}
	return Db{
		firestore: client,
	}
}

// ProvideFirestore returns a *firestore.Client
func initFirestore(ctx context.Context) (*firestore.Client, error) {
	var (
		app  *firebase.App
		json []byte
		err  error
	)
	conf := &firebase.Config{ProjectID: os.Getenv("GCP_PROJECT")}
	if os.Getenv("GCP_CREDENTIALS") != "" {
		// import from env
		log.Info("Importing GCP credentials from env")
		json, err = b64.StdEncoding.DecodeString(os.Getenv("GCP_CREDENTIALS"))
		if err != nil {
			log.Fatal(err)
		}
		app, err = firebase.NewApp(ctx, conf, option.WithCredentialsJSON([]byte(json)))
		if err != nil {
			log.Fatal(err)
		}
	} else {
		// local dev/application-default case
		app, err = firebase.NewApp(ctx, conf)
		if err != nil {
			log.Fatal(err)
		}
	}

	client, err := app.Firestore(ctx)
	if err != nil {
		return nil, err
	}

	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		log.Println("🚨 Production Firestore Host (cloud) is activated")
	} else {
		log.Println("🧪 Emulator Firestore Host is activated")
	}
	return client, nil
}

func (db Db) SaveFundingReceipt(ctx context.Context, newReceipt FundingReceipt) error {
	if os.Getenv("DEBUG") != "" {
		// don't accumulate receipts in debug mode
		return nil
	}
	table := db.firestore.Collection("funding-receipts")
	ref := table.Doc(mkPKEY(newReceipt.Username, newReceipt.ChainPrefix))

	_, err := ref.Set(ctx, newReceipt)
	return err
}

func (db Db) PruneExpiredReceipts(ctx context.Context, beforeFundingTime time.Time) (int, error) {
	table := db.firestore.Collection("funding-receipts")

	iter := table.Documents(ctx)
	defer iter.Stop()

	var numPruned int
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return numPruned, err
		}

		var receipt FundingReceipt
		err = doc.DataTo(&receipt)
		if err != nil {
			return numPruned, err
		}

		if receipt.FundedAt.Before(beforeFundingTime) {
			_, err = doc.Ref.Delete(ctx)
			if err != nil {
				return numPruned, err
			}
			numPruned += 1
		}
	}

	return numPruned, nil
}

func (db Db) GetFundingReceiptByUsernameAndChainPrefix(ctx context.Context, username string, chainPrefix string) (*FundingReceipt, error) {
	if os.Getenv("DEBUG") != "" {
		// allow unlimited faucet tapping in debug mode
		return nil, nil
	}

	table := db.firestore.Collection("funding-receipts")
	ref := table.Doc(mkPKEY(username, chainPrefix))

	doc, err := ref.Get(ctx)
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out FundingReceipt
	err = doc.DataTo(&out)
	if err != nil {
		return nil, err
	}

	return &out, nil
}

func mkPKEY(username string, chainPrefix string) string {
	return getMD5Hash(username + chainPrefix)
}

func getMD5Hash(text string) string {
	hash := md5.Sum([]byte(text))
	return hex.EncodeToString(hash[:])
}
