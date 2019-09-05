package qln

import (
	"bytes"
	"bufio"
	"fmt"
	"time"
	"os"

	//"github.com/mit-dci/lit/btcutil/chaincfg/chainhash"
	"github.com/mit-dci/lit/btcutil/txscript"
	//"github.com/mit-dci/lit/crypto/fastsha256"
	//"github.com/mit-dci/lit/crypto/koblitz"
	"github.com/mit-dci/lit/lnutil"
	"github.com/mit-dci/lit/logging"
	//"github.com/mit-dci/lit/portxo"
	"github.com/mit-dci/lit/sig64"
	"github.com/mit-dci/lit/wire"
	"github.com/mit-dci/lit/btcutil/txsort"
)




// CoopClose requests a cooperative close of the channel
func (nd *LitNode) CoopClose(q *Qchan) error {

	fmt.Printf("::%s:: CoopClose() ----START----: qln/close.go \n",os.Args[6][len(os.Args[6])-4:])

	nd.RemoteMtx.Lock()
	_, ok := nd.RemoteCons[q.Peer()]
	nd.RemoteMtx.Unlock()
	if !ok {
		return fmt.Errorf("not connected to peer %d ", q.Peer())
	}


	//=================================================================
	//=================================================================
	//=================================================================

	// tx, err := q.SimpleCloseTx()
	// if err != nil {
	// 	return err
	// }


	tx := wire.NewMsgTx()
	tx.Version = 2

	
	fmt.Printf("::%s:: SimpleCloseTx(): qln/close.go: q.Op for TxIn %+v \n",os.Args[6][len(os.Args[6])-4:], q.Op)

	tx.AddTxIn(wire.NewTxIn(&q.Op, nil, nil))

	//--------------------------------------------------

	fmt.Printf("::%s:: SimpleCloseTx(): qln/close.go: q.MyRefundPub %x, q.TheirRefundPub %x,  \n",os.Args[6][len(os.Args[6])-4:], q.MyRefundPub, q.TheirRefundPub)

	myScript := lnutil.DirectWPKHScript(q.MyRefundPub)
	fmt.Printf("::%s:: SimpleCloseTx(): qln/close.go: DirectWPKHScript: myScript: %x \n",os.Args[6][len(os.Args[6])-4:], myScript)
	
	myOutput := wire.NewTxOut(100000, myScript)
	tx.AddTxOut(myOutput)

	//--------------------------------------------------

	theirScript := lnutil.DirectWPKHScript(q.TheirRefundPub)
	fmt.Printf("::%s:: SimpleCloseTx(): qln/close: DirectWPKHScript: theirScript: %x \n",os.Args[6][len(os.Args[6])-4:], theirScript)

	theirOutput := wire.NewTxOut(100000, theirScript)
	tx.AddTxOut(theirOutput)

	//--------------------------------------------------


	txsort.InPlaceSort(tx)


	//=================================================================

	sig, err := nd.SignSimpleClose(q, tx)
	if err != nil {
		return err
	}



	//=================================================================
	//=================================================================
	//=================================================================	


	nd.RemoteMtx.Lock()
	q.LastUpdate = uint64(time.Now().UnixNano() / 1000)
	q.CloseData.Closed = true
	q.CloseData.CloseTxid = tx.TxHash()
	nd.RemoteMtx.Unlock()
	err = nd.SaveQchanUtxoData(q)
	if err != nil {
		return err
	}

	var signature [64]byte
	copy(signature[:], sig[:])



	outMsg := lnutil.NewCloseReqMsg(q.Peer(), q.Op, signature)
	nd.tmpSendLitMsg(outMsg)

	fmt.Printf("::%s:: CoopClose() ----END----: qln/close.go \n",os.Args[6][len(os.Args[6])-4:])

	return nil
}




//+++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++



func (nd *LitNode) CloseReqHandler(msg lnutil.CloseReqMsg) {
	opArr := lnutil.OutPointToBytes(msg.Outpoint)

	fmt.Printf("::%s:: CloseReqHandler() ----START----: qln/close.go \n",os.Args[6][len(os.Args[6])-4:])

	// get channel
	q, err := nd.GetQchan(opArr)
	if err != nil {
		logging.Errorf("CloseReqHandler GetQchan err %s", err.Error())
		return
	}

	if nd.SubWallet[q.Coin()] == nil {
		logging.Errorf("Not connected to coin type %d\n", q.Coin())
	}



	//=================================================================
	//=================================================================
	//=================================================================	

	// build close tx
	tx, err := q.SimpleCloseTx()
	if err != nil {
		logging.Errorf("CloseReqHandler SimpleCloseTx err %s", err.Error())
		return
	}



	//=================================================================


	// sign close
	mySig, err := nd.SignSimpleClose(q, tx)
	if err != nil {
		logging.Errorf("CloseReqHandler SignSimpleClose err %s", err.Error())
		return
	}


	//=================================================================
	//=================================================================
	//=================================================================	




	myBigSig := sig64.SigDecompress(mySig)

	theirBigSig := sig64.SigDecompress(msg.Signature)



	myBigSig = append(myBigSig, byte(txscript.SigHashAll))

	theirBigSig = append(theirBigSig, byte(txscript.SigHashAll))



	fmt.Printf("::%s:: FundTxScript(): CloseReqHandler(): qln/close.go: q.MyPub %x, q.TheirPub %x \n",os.Args[6][len(os.Args[6])-4:], q.MyPub, q.TheirPub)

	pre, swap, err := lnutil.FundTxScript(q.MyPub, q.TheirPub)
	if err != nil {
		logging.Errorf("CloseReqHandler FundTxScript err %s", err.Error())
		return
	}

		

	// swap if needed
	if swap {
		fmt.Printf("::%s:: CloseReqHandler(): qln/close.go: SpendMultiSigWitStack: swap: %t, cap(theirBigSig) %d, cap(myBigSig) %d \n",os.Args[6][len(os.Args[6])-4:], swap, cap(theirBigSig), cap(myBigSig))
		tx.TxIn[0].Witness = SpendMultiSigWitStack(pre, theirBigSig, myBigSig)
	} else {
		fmt.Printf("::%s:: CloseReqHandler(): qln/close.go: SpendMultiSigWitStack: swap: %t, cap(myBigSig) %d, cap(theirBigSig) %d \n",os.Args[6][len(os.Args[6])-4:], swap, cap(myBigSig), cap(theirBigSig))
		tx.TxIn[0].Witness = SpendMultiSigWitStack(pre, myBigSig, theirBigSig)
	}
	logging.Info(lnutil.TxToString(tx))



	//==========================================

	// save channel state to db as closed.
	nd.RemoteMtx.Lock()
	q.LastUpdate = uint64(time.Now().UnixNano() / 1000)
	q.CloseData.Closed = true
	q.CloseData.CloseTxid = tx.TxHash()
	nd.RemoteMtx.Unlock()
	err = nd.SaveQchanUtxoData(q)
	if err != nil {
		logging.Errorf("CloseReqHandler SaveQchanUtxoData err %s", err.Error())
		return
	}


	//==========================================

	fmt.Printf("::%s:: CloseReqHandler(): qln/close.go: lnutil.TxToString(tx) %s \n",os.Args[6][len(os.Args[6])-4:], lnutil.TxToString(tx))


	var buft bytes.Buffer
	wtt := bufio.NewWriter(&buft)
	tx.Serialize(wtt)
	wtt.Flush()


	fmt.Printf("::%s:: CloseReqHandler(): qln/close.go: tx %x \n",os.Args[6][len(os.Args[6])-4:], buft.Bytes())

	// broadcast
	err = nd.SubWallet[q.Coin()].PushTx(tx)
	if err != nil {
		logging.Errorf("CloseReqHandler NewOutgoingTx err %s", err.Error())
		return
	}

	peerIdx := q.Peer()
	peer := nd.PeerMan.GetPeerByIdx(int32(peerIdx))

	// Broadcast that we've closed a channel
	closed := ChannelStateUpdateEvent{
		Action:   "closed",
		ChanIdx:  q.Idx(),
		State:    q.State,
		TheirPub: peer.GetPubkey(),
		CoinType: q.Coin(),
	}

	if succeed, err := nd.Events.Publish(closed); err != nil {
		logging.Errorf("ClosedHandler publish err %s", err)
		return
	} else if !succeed {
		logging.Errorf("ClosedHandler publish did not succeed")
		return
	}

	fmt.Printf("::%s:: CloseReqHandler() ----END----: qln/close.go \n",os.Args[6][len(os.Args[6])-4:])

	return
}

