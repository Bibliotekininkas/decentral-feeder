package kwilhelper

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kwilteam/kwil-db/core/client"
	"github.com/kwilteam/kwil-db/core/crypto"
	"github.com/kwilteam/kwil-db/core/crypto/auth"
	"github.com/kwilteam/kwil-db/core/types/transactions"
)

func CheckTx(cl *client.Client, ctx context.Context, txHash []byte, action string) {
	res, err := cl.WaitTx(ctx, txHash, 250*time.Millisecond)
	if err != nil {
		log.Fatalf("Failed to wait for transaction %x: %v", txHash, err)
	}
	if res.TxResult.Code == transactions.CodeOk.Uint32() {
		fmt.Printf("Success: %q in transaction %x\n", action, txHash)
	} else {
		log.Fatalf("Fail: %q in transaction %x, Result code %d, log: %q",
			action, txHash, res.TxResult.Code, res.TxResult.Log)
	}
}

func MakeEthSigner(keyHex string) auth.Signer {
	key, err := crypto.Secp256k1PrivateKeyFromHex(keyHex)
	if err != nil {
		panic(fmt.Sprintf("bad private key: %v", err))
	}
	return &auth.EthPersonalSigner{Key: *key}
}
