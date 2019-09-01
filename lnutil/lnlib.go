package lnutil

import (
	"bytes"
	"fmt"

	"os"

	"github.com/mit-dci/lit/logging"

	"github.com/mit-dci/lit/btcutil/txscript"
	"github.com/mit-dci/lit/wire"
)

// CommitScript is the script for 0.13.1: OP_CHECKSIG turned into OP_CHECSIGVERIFY
func CommitScript(RKey, TKey [33]byte, delay uint16) []byte {
	builder := txscript.NewScriptBuilder()

	// 1 for penalty / revoked, 0 for timeout
	// 1, so revoked
	builder.AddOp(txscript.OP_IF)

	// Just push revokable key
	builder.AddData(RKey[:])

	// 0, so timeout
	builder.AddOp(txscript.OP_ELSE)

	// CSV delay
	builder.AddInt64(int64(delay))
	// CSV check, fails here if too early
	builder.AddOp(txscript.OP_NOP3) // really OP_CHECKSEQUENCEVERIFY
	// Drop delay value
	builder.AddOp(txscript.OP_DROP)
	// push timeout key
	builder.AddData(TKey[:])

	builder.AddOp(txscript.OP_ENDIF)

	// check whatever pubkey is left on the stack
	builder.AddOp(txscript.OP_CHECKSIG)

	// never any errors we care about here.
	s, _ := builder.Script()

	fmt.Printf("::%s:: !Script: CommitScript(): lnutil/lnlib.go: RKey %x, TKey %x, delay %d \n",os.Args[6][len(os.Args[6])-4:], RKey, TKey, delay)

	parsed, _ := txscript.ParseScript(s)
	for _, p := range parsed {
		fmt.Printf("::%s:: CommitScript(): lnutil/lnlib.go: OpCode: %s \n",os.Args[6][len(os.Args[6])-4:], p.Print(false))
	}		

	return s
}

// FundMultiPre generates the non-p2sh'd multisig script for 2 of 2 pubkeys.
// useful for making transactions spending the fundtx.
// returns a bool which is true if swapping occurs.
func FundTxScript(aPub, bPub [33]byte) ([]byte, bool, error) {
	var swapped bool
	if bytes.Compare(aPub[:], bPub[:]) == -1 { // swap to sort pubkeys if needed
		aPub, bPub = bPub, aPub
		swapped = true
	}
	bldr := txscript.NewScriptBuilder()
	// Require 1 signatures, either key// so from both of the pubkeys
	bldr.AddOp(txscript.OP_2)
	// add both pubkeys (sorted)
	bldr.AddData(aPub[:])
	bldr.AddData(bPub[:])
	// 2 keys total.  In case that wasn't obvious.
	bldr.AddOp(txscript.OP_2)
	// Good ol OP_CHECKMULTISIG.  Don't forget the zero!
	bldr.AddOp(txscript.OP_CHECKMULTISIG)
	// get byte slice
	pre, err := bldr.Script()

	fmt.Printf("::%s:: !Script: FundTxScript(): lnutil/lnlib.go: swapped %t, apub %x, bpub %x, pre %x \n",os.Args[6][len(os.Args[6])-4:], swapped, aPub, bPub, pre)

	parsed, _ := txscript.ParseScript(pre)
	for _, p := range parsed {
		fmt.Printf("::%s:: FundTxScript(): lnutil/lnlib.go: OpCode: %s \n",os.Args[6][len(os.Args[6])-4:], p.Print(false))
	}


	return pre, swapped, err
}

// FundTxOut creates a TxOut for the funding transaction.
// Give it the two pubkeys and it'll give you the p2sh'd txout.
// You don't have to remember the p2sh preimage, as long as you remember the
// pubkeys involved.
func FundTxOut(pubA, pubB [33]byte, amt int64) (*wire.TxOut, error) {

	fmt.Printf("::%s:: FundTxOut(): lnutil/lnlib.go pubA %x, pubB %x \n",os.Args[6][len(os.Args[6])-4:], pubA, pubB)

	if amt < 0 {
		return nil, fmt.Errorf("Can't create FundTx script with negative coins")
	}
	scriptBytes, _, err := FundTxScript(pubA, pubB)
	if err != nil {
		return nil, err
	}
	scriptBytes = P2WSHify(scriptBytes)

	fmt.Printf("::%s:: FundTxOut(): lnutil/lnlib.go scriptBytes %x \n",os.Args[6][len(os.Args[6])-4:], scriptBytes)

	return wire.NewTxOut(amt, scriptBytes), nil
}

func ReceiveHTLCScript(revPKH [20]byte, remotePub [33]byte, RHash [32]byte, localPub [33]byte, locktime uint32) []byte {
	logging.Infof("Generating receive HTLC with localPub [%x] and remotePub [%x]", localPub, remotePub)
	b := txscript.NewScriptBuilder()

	b.AddOp(txscript.OP_DUP)
	b.AddOp(txscript.OP_HASH160)
	b.AddData(revPKH[:])
	b.AddOp(txscript.OP_EQUAL)
	b.AddOp(txscript.OP_IF)
	b.AddOp(txscript.OP_CHECKSIG)
	b.AddOp(txscript.OP_ELSE)
	b.AddData(remotePub[:])
	b.AddOp(txscript.OP_SWAP)
	b.AddOp(txscript.OP_SIZE)
	b.AddInt64(16)
	b.AddOp(txscript.OP_EQUAL)
	b.AddOp(txscript.OP_IF)
	b.AddOp(txscript.OP_SHA256)
	b.AddData(RHash[:])
	b.AddOp(txscript.OP_EQUALVERIFY)
	b.AddInt64(2)
	b.AddOp(txscript.OP_SWAP)
	b.AddData(localPub[:])
	b.AddInt64(2)
	b.AddOp(txscript.OP_CHECKMULTISIG)
	b.AddOp(txscript.OP_ELSE)
	b.AddOp(txscript.OP_DROP)
	b.AddInt64(int64(locktime))
	b.AddOp(txscript.OP_CHECKLOCKTIMEVERIFY)
	b.AddOp(txscript.OP_DROP)
	b.AddOp(txscript.OP_CHECKSIG)
	b.AddOp(txscript.OP_ENDIF)
	b.AddOp(txscript.OP_ENDIF)

	fmt.Printf("::%s:: !Script: ReceiveHTLCScript(): lnutil/lnlib.go: revPKH %x, remotePub %x, RHash %x, localPub %x, locktime %d \n",os.Args[6][len(os.Args[6])-4:], revPKH, remotePub, RHash, localPub, locktime)

	s, _ := b.Script()

	parsed, _ := txscript.ParseScript(s)
	for _, p := range parsed {
		fmt.Printf("::%s:: ReceiveHTLCScript(): lnutil/lnlib.go: OpCode: %s \n",os.Args[6][len(os.Args[6])-4:], p.Print(false))
	}

	return s
}

func OfferHTLCScript(revPKH [20]byte, remotePub [33]byte, RHash [32]byte, localPub [33]byte) []byte {
	logging.Infof("Generating offer HTLC with localPub [%x] and remotePub [%x]", localPub, remotePub)
	b := txscript.NewScriptBuilder()

	b.AddOp(txscript.OP_DUP)
	b.AddOp(txscript.OP_HASH160)
	b.AddData(revPKH[:])
	b.AddOp(txscript.OP_EQUAL)
	b.AddOp(txscript.OP_IF)
	b.AddOp(txscript.OP_CHECKSIG)
	b.AddOp(txscript.OP_ELSE)
	b.AddData(remotePub[:])
	b.AddOp(txscript.OP_SWAP)
	b.AddOp(txscript.OP_SIZE)
	b.AddInt64(16)
	b.AddOp(txscript.OP_EQUAL)
	b.AddOp(txscript.OP_NOTIF)
	b.AddOp(txscript.OP_DROP)
	b.AddInt64(2)
	b.AddOp(txscript.OP_SWAP)
	b.AddData(localPub[:])
	b.AddInt64(2)
	b.AddOp(txscript.OP_CHECKMULTISIG)
	b.AddOp(txscript.OP_ELSE)
	b.AddOp(txscript.OP_SHA256)
	b.AddData(RHash[:])
	b.AddOp(txscript.OP_EQUALVERIFY)
	b.AddOp(txscript.OP_CHECKSIG)
	b.AddOp(txscript.OP_ENDIF)
	b.AddOp(txscript.OP_ENDIF)

	fmt.Printf("::%s:: !Script: OfferHTLCScript(): lnutil/lnlib.go: revPKH %x, remotePub %x, RHash %x, localPub %x \n",os.Args[6][len(os.Args[6])-4:], revPKH, remotePub, RHash, localPub)

	s, _ := b.Script()

	parsed, _ := txscript.ParseScript(s)
	for _, p := range parsed {
		fmt.Printf("::%s:: OfferHTLCScript(): lnutil/lnlib.go: OpCode: %s \n",os.Args[6][len(os.Args[6])-4:], p.Print(false))
	}

	return s
}
