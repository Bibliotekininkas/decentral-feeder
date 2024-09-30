package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kwilteam/kwil-db/core/client"
	"github.com/kwilteam/kwil-db/core/crypto"
	"github.com/kwilteam/kwil-db/core/crypto/auth"
	klog "github.com/kwilteam/kwil-db/core/log"
	ctypes "github.com/kwilteam/kwil-db/core/types/client"
	"github.com/kwilteam/kwil-db/core/utils"
	"github.com/kwilteam/kwil-db/parse"
)

const (
	chainID  = "kwil-chain-nSRNXdbH"
	provider = "http://127.0.0.1:8484"
	privKey  = "9167061a722d41dd5fb374c37bd6ed10ddd1b46c7d0016a5aaedae83c520fb00"
)

func main() {
	ctx := context.Background()
	signer := makeEthSigner(privKey)
	acctID := signer.Identity() // acctID remains as []byte

	opts := &ctypes.Options{
		Logger:  klog.NewStdOut(klog.InfoLevel),
		ChainID: chainID,
		Signer:  signer,
	}

	// Create the client and connect to the RPC provider.
	cl, err := client.NewClient(ctx, provider, opts)
	if err != nil {
		log.Fatal(err)
	}

	// Define the database name.
	dbName := "was_here"
	dbID := utils.GenerateDBID(dbName, acctID)

	// Drop and redeploy the database with the updated schema.
	deployDatabase(cl, ctx, dbName, acctID)

	// Insert data into the database using the "tag" action.
	const tagAction = "tag"
	data := "test message4"
	fmt.Printf("Inserting data into database %v using action %q...\n", dbName, tagAction)
	txHash, err := cl.Execute(ctx, dbID, tagAction, [][]any{{data}}, ctypes.WithSyncBroadcast(true))
	if err != nil {
		log.Fatal(err)
	}
	// Wait for the transaction to be included in a block.
	err = waitForTx(cl, ctx, txHash)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Data inserted successfully.\n")

	// Retrieve and display the data.
	retrieveData(cl, ctx, dbID)
}

func makeEthSigner(keyHex string) auth.Signer {
	key, err := crypto.Secp256k1PrivateKeyFromHex(keyHex)
	if err != nil {
		panic(fmt.Sprintf("bad private key: %v", err))
	}
	return &auth.EthPersonalSigner{Key: *key}
}

func waitForTx(cl *client.Client, ctx context.Context, txHash []byte) error {
	res, err := cl.WaitTx(ctx, txHash, 250*time.Millisecond)
	if err != nil {
		return fmt.Errorf("failed to wait for transaction: %v", err)
	}
	if res.TxResult.Code != 0 {
		return fmt.Errorf("transaction failed with code %d, log: %s", res.TxResult.Code, res.TxResult.Log)
	}
	return nil
}

func deployDatabase(cl *client.Client, ctx context.Context, dbName string, acctID []byte) {
	// Check if the database exists.
	databases, err := cl.ListDatabases(ctx, acctID)
	if err != nil {
		log.Fatal(err)
	}

	deployed := false
	for _, db := range databases {
		if db.Name == dbName {
			deployed = true
			break
		}
	}

	if deployed {
		// Drop the existing database.
		fmt.Printf("Dropping existing database: %v...\n", dbName)
		txHash, err := cl.DropDatabase(ctx, dbName, ctypes.WithSyncBroadcast(true))
		if err != nil {
			log.Fatal(err)
		}
		// Wait for the transaction to be included in a block.
		err = waitForTx(cl, ctx, txHash)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Database %v dropped successfully.\n", dbName)
	}

	// Deploy the updated database.
	fmt.Printf("Deploying database: %v...\n", dbName)

	// Parse the updated Kuneiform schema.
	schema, err := parse.Parse([]byte(testKf))
	if err != nil {
		log.Fatal(err)
	}

	// Deploy the database.
	txHash, err := cl.DeployDatabase(ctx, schema, ctypes.WithSyncBroadcast(true))
	if err != nil {
		log.Fatal(err)
	}
	// Wait for the transaction to be included in a block.
	err = waitForTx(cl, ctx, txHash)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Database %v deployed successfully.\n", dbName)
}

func retrieveData(cl *client.Client, ctx context.Context, dbID string) {
	const getAllAction = "get_all"
	fmt.Printf("Retrieving all data using action %q...\n", getAllAction)
	records, err := cl.Call(ctx, dbID, getAllAction, nil)
	if err != nil {
		log.Fatal(err)
	}

	// Use ExportString() to get the data
	if tab := records.Records.ExportString(); len(tab) == 0 {
		fmt.Println("No data records in table.")
	} else {
		fmt.Println("All entries in tags table:")
		var headers []string
		for k := range tab[0] {
			headers = append(headers, k)
		}
		fmt.Printf("Column names: %#v\nValues:\n", headers)
		for _, row := range tab {
			var rowVals []string
			for _, h := range headers {
				rowVals = append(rowVals, row[h])
			}
			fmt.Printf("%#v\n", rowVals)
		}
	}
}

var testKf = `database was_here;

table tags {
    id uuid primary key,
    ident text not null,
    val int default(42),
    msg text not null
}

action tag($msg) public {
    INSERT INTO tags (id, ident, msg) VALUES (
        uuid_generate_v5('69c7f28c-b681-4d89-b4d9-8c8211065585'::uuid, @txid),
        @caller,
        $msg);
}

action delete_mine() public {
    DELETE FROM tags WHERE ident = @caller;
}

action delete_other ($ident) public owner {
    DELETE FROM "tags" WHERE ident = $ident;
}

action delete_all () public owner {
    DELETE FROM tags;
}

action get_user_tag($ident) public view {
    SELECT msg, val FROM tags WHERE ident = $ident;
}

action get_my_tag() public view {
    SELECT msg, val FROM tags WHERE ident = @caller;
}

action get_all() public view {
    SELECT * FROM tags;
}
`
