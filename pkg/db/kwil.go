package db

import (
	"context"
	"os"
	"time"

	kwilhelper "github.com/diadata-org/decentral-feeder/pkg/helpers/kwil"
	"github.com/kwilteam/kwil-db/core/client"
	klog "github.com/kwilteam/kwil-db/core/log"
	ctypes "github.com/kwilteam/kwil-db/core/types/client"
	"github.com/kwilteam/kwil-db/core/utils"
	"github.com/kwilteam/kwil-db/parse"
	log "github.com/sirupsen/logrus"
)

func GetKwilClient(chainID string, provider string) *client.Client {
	privKey := os.Getenv("PRIV_KEY")
	if privKey == "" {
		log.Fatal("Private key not found in environment variables")
	}
	ctx := context.Background()
	signer := kwilhelper.MakeEthSigner(privKey)

	opts := &ctypes.Options{
		Logger:  klog.NewStdOut(klog.InfoLevel),
		ChainID: chainID,
		Signer:  signer,
	}

	cl, err := client.NewClient(ctx, provider, opts)
	if err != nil {
		log.Fatal(err)
	}
	return cl
}

func DeployDatabase(cl *client.Client, dbName string, dbSchema string) (string, bool, error) {
	ctx := context.Background()
	acctID := cl.Signer.Identity()

	dbID := utils.GenerateDBID(dbName, acctID)

	// Check if the database is already deployed
	databases, err := cl.ListDatabases(ctx, acctID)
	if err != nil {
		return dbID, false, err
	}

	log.Info("List deployed databases.")
	for _, db := range databases {
		log.Infof("db name -- owner: %s -- %s.", db.Name, db.Owner)
	}

	// Use a loop to check if the database is deployed
	var deployed bool
	for _, db := range databases {
		if db.Name == dbName {
			deployed = true
			return dbID, deployed, nil
		}
	}

	if !deployed {
		log.Infof("Deploying database: %v...", dbName)

		// Parse the Kuneiform schema
		schema, err := parse.Parse([]byte(dbSchema))
		if err != nil {
			return dbID, false, err
		}

		// Deploy the database
		txHash, err := cl.DeployDatabase(ctx, schema)
		if err != nil {
			return dbID, false, err
		}

		// Set a timeout context for waiting on the transaction
		ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		kwilhelper.CheckTx(cl, ctxWithTimeout, txHash, "deploy database")
	}

	return dbID, false, nil
}
