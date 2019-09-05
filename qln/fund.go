package qln

import (
	"fmt"
	"time"

	"os"

	"github.com/mit-dci/lit/consts"
	//"github.com/mit-dci/lit/crypto/koblitz"
	//"github.com/mit-dci/lit/elkrem"
	"github.com/mit-dci/lit/lnutil"
	"github.com/mit-dci/lit/logging"
	"github.com/mit-dci/lit/portxo"
	"github.com/mit-dci/lit/wire"
)


func (nd *LitNode) FundChannel(
	peerIdx, cointype uint32, ccap, initSend int64, data [32]byte) (uint32, error) {

	fmt.Printf("::%s:: FundChannel() ----START----: qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])

	_, ok := nd.SubWallet[cointype]
	if !ok {
		return 0, fmt.Errorf("No wallet of type %d connected", cointype)
	}

	nd.InProg.mtx.Lock()
	//	defer nd.InProg.mtx.Lock()

	_, ok = nd.ConnectedCoinTypes[cointype]
	if !ok {
		nd.InProg.mtx.Unlock()
		return 0, fmt.Errorf("No daemon of type %d connected. Can't fund, only receive", cointype)
	}

	fee := nd.SubWallet[cointype].Fee() * 1000

	if nd.InProg.PeerIdx != 0 {
		nd.InProg.mtx.Unlock()
		return 0, fmt.Errorf("fund with peer %d not done yet", nd.InProg.PeerIdx)
	}

	if initSend < 0 || ccap < 0 {
		nd.InProg.mtx.Unlock()
		return 0, fmt.Errorf("Can't have negative send or capacity")
	}
	if ccap < consts.MinChanCapacity { // limit for now
		nd.InProg.mtx.Unlock()
		return 0, fmt.Errorf("Min channel capacity 1M sat")
	}
	if initSend > ccap {
		nd.InProg.mtx.Unlock()
		return 0, fmt.Errorf("Can't send %d in %d capacity channel", initSend, ccap)
	}

	if initSend != 0 && initSend < consts.MinOutput+fee {
		nd.InProg.mtx.Unlock()
		return 0, fmt.Errorf("Can't send %d as initial send because MinOutput is %d", initSend, consts.MinOutput+fee)
	}

	if ccap-initSend < consts.MinOutput+fee {
		nd.InProg.mtx.Unlock()
		return 0, fmt.Errorf("Can't send %d as initial send because MinOutput is %d and you would only have %d", initSend, consts.MinOutput+fee, ccap-initSend)
	}

	// TODO - would be convenient if it auto connected to the peer huh
	if !nd.ConnectedToPeer(peerIdx) {
		nd.InProg.mtx.Unlock()
		return 0, fmt.Errorf("Not connected to peer %d. Do that yourself.", peerIdx)
	}

	cIdx, err := nd.NextChannelIdx()
	if err != nil {
		nd.InProg.mtx.Unlock()
		return 0, err
	}

	logging.Infof("next channel idx: %d", cIdx)

	nd.InProg.ChanIdx = cIdx
	nd.InProg.PeerIdx = peerIdx
	nd.InProg.Amt = ccap
	nd.InProg.InitSend = initSend
	nd.InProg.Data = data

	nd.InProg.Coin = cointype
	nd.InProg.mtx.Unlock() // switch to defer

	outMsg := lnutil.NewPointReqMsg(peerIdx, cointype)

	nd.tmpSendLitMsg(outMsg)

	// wait until it's done!
	idx := <-nd.InProg.done

	fmt.Printf("::%s:: FundChannel() ----END----: qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])

	return idx, nil
}

// RECIPIENT

func (nd *LitNode) PointReqHandler(msg lnutil.PointReqMsg) {

	fmt.Printf("::%s:: PointReqHandler() ----START----: qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])


	cIdx, err := nd.NextChannelIdx()
	if err != nil {
		logging.Errorf("PointReqHandler err %s", err.Error())
		return
	}

	_, ok := nd.SubWallet[msg.Cointype]
	if !ok {
		logging.Errorf("PointReqHandler err no wallet for type %d", msg.Cointype)
		return
	}

	var kg portxo.KeyGen
	kg.Depth = 5
	kg.Step[0] = 44 | 1<<31
	kg.Step[1] = msg.Cointype | 1<<31
	kg.Step[2] = UseChannelFund
	kg.Step[3] = msg.Peer() | 1<<31
	kg.Step[4] = cIdx | 1<<31

	myChanPub, _ := nd.GetUsePub(kg, UseChannelFund)

	myRefundPub, _ := nd.GetUsePub(kg, UseChannelRefund)


	outMsg := lnutil.NewPointRespMsg(msg.Peer(), myChanPub, myRefundPub)
	nd.tmpSendLitMsg(outMsg)

	fmt.Printf("::%s:: PointReqHandler() ----END----: qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])

	return
}

// FUNDER
func (nd *LitNode) PointRespHandler(msg lnutil.PointRespMsg) error {

	fmt.Printf("::%s:: PointRespHandler() ----START----: qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])

	var err error
	logging.Infof("Got PointResponse")

	nd.InProg.mtx.Lock()
	defer nd.InProg.mtx.Unlock()

	if nd.InProg.PeerIdx == 0 {
		return fmt.Errorf("Got point response but no channel creation in progress")
	}

	if nd.InProg.PeerIdx != msg.Peer() {
		return fmt.Errorf(
			"making channel with peer %d but got PointResp from %d",
			nd.InProg.PeerIdx, msg.Peer())
	}

	if nd.SubWallet[nd.InProg.Coin] == nil {
		return fmt.Errorf("Not connected to coin type %d\n", nd.InProg.Coin)
	}

	q := new(Qchan)

	q.Height = -1

	q.Value = nd.InProg.Amt

	q.KeyGen.Depth = 5
	q.KeyGen.Step[0] = 44 | 1<<31
	q.KeyGen.Step[1] = nd.InProg.Coin | 1<<31
	q.KeyGen.Step[2] = UseChannelFund

	fmt.Printf("::%s:: PointRespHandler(): qln/fund.go: q.KeyGen.Step[2] = UseChannelFund: %d \n",os.Args[6][len(os.Args[6])-4:], UseChannelFund)

	q.KeyGen.Step[3] = nd.InProg.PeerIdx | 1<<31
	q.KeyGen.Step[4] = nd.InProg.ChanIdx | 1<<31

	q.MyPub, _ = nd.GetUsePub(q.KeyGen, UseChannelFund)
	q.MyRefundPub, _ = nd.GetUsePub(q.KeyGen, UseChannelRefund)

	fmt.Printf("::%s:: PointRespHandler(): qln/fund.go: q.MyPub: %x \n",os.Args[6][len(os.Args[6])-4:], q.MyPub)
	fmt.Printf("::%s:: PointRespHandler(): qln/fund.go: q.MyRefundPub: %x \n",os.Args[6][len(os.Args[6])-4:], q.MyRefundPub)


	copy(q.TheirPub[:], msg.ChannelPub[:])



	fmt.Printf("::%s:: PointRespHandler(): qln/fund.go: q.TheirPub: %x \n",os.Args[6][len(os.Args[6])-4:], q.TheirPub)
	fmt.Printf("::%s:: PointRespHandler(): qln/fund.go: q.TheirRefundPub: %x \n",os.Args[6][len(os.Args[6])-4:], q.TheirRefundPub)




	q.LastUpdate = uint64(time.Now().UnixNano() / 1000)


	fmt.Printf("::%s:: PointRespHandler(): FundTxOut() qln/fund.go: q.MyPub %x, q.TheirPub: %x \n",os.Args[6][len(os.Args[6])-4:], q.MyPub, q.TheirPub)

	// get txo for channel
	txo, err := lnutil.FundTxOut(q.MyPub, q.TheirPub, nd.InProg.Amt)
	if err != nil {
		return err
	}

	fmt.Printf("::%s:: PointRespHandler()1: FundTxOut() qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])

	outPoints, err := nd.SubWallet[q.Coin()].MaybeSend([]*wire.TxOut{txo}, true)
	if err != nil {
		return err
	}

	fmt.Printf("::%s:: PointRespHandler()2: FundTxOut() qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])

	// should only have 1 txout index from MaybeSend, which we use
	if len(outPoints) != 1 {
		return fmt.Errorf("got %d OPs from MaybeSend (expect 1)", len(outPoints))
	}

	// save fund outpoint to inProg
	nd.InProg.op = outPoints[0]
	// also set outpoint in channel
	q.Op = *nd.InProg.op

	fmt.Printf("::%s:: opz PointRespHandler(): q.Op %+v \n",os.Args[6][len(os.Args[6])-4:], q.Op)

	// // create initial state for elkrem points
	q.State = new(StatCom)


	fmt.Printf("::%s:: PointRespHandler()3: FundTxOut() qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])


	// save channel to db
	err = nd.SaveQChan(q)
	if err != nil {
		nd.FailChannel(q)
		return fmt.Errorf("PointRespHandler SaveQchanState err %s", err.Error())
	}

	fmt.Printf("::%s:: PointRespHandler()4: FundTxOut() qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])



	fmt.Printf("::%s:: PointRespHandler()5: FundTxOut() qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])


	outMsg := lnutil.NewChanDescMsg(
		msg.Peer(), *nd.InProg.op, q.MyPub, q.MyRefundPub,
		nd.InProg.Coin, nd.InProg.Amt, nd.InProg.InitSend, nd.InProg.Data)

	nd.tmpSendLitMsg(outMsg)

	fmt.Printf("::%s:: PointRespHandler() ----END----: qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])

	return nil
}

// RECIPIENT

func (nd *LitNode) QChanDescHandler(msg lnutil.ChanDescMsg) error {

	fmt.Printf("::%s:: QChanDescHandler() ----START----: qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])

	wal, ok := nd.SubWallet[msg.CoinType]
	if !ok {
		return fmt.Errorf("QChanDescHandler err no wallet for type %d", msg.CoinType)
	}

	fmt.Printf("::%s:: opz QChanDescHandler(): msg.Outpoint %+v \n",os.Args[6][len(os.Args[6])-4:], msg.Outpoint)

	// deserialize desc
	op := msg.Outpoint
	opArr := lnutil.OutPointToBytes(op)
	amt := msg.Capacity

	cIdx, err := nd.NextChannelIdx()
	if err != nil {
		return fmt.Errorf("QChanDescHandler err %s", err.Error())
	}

	qc := new(Qchan)

	qc.Height = -1
	qc.KeyGen.Depth = 5
	qc.KeyGen.Step[0] = 44 | 1<<31
	qc.KeyGen.Step[1] = msg.CoinType | 1<<31
	qc.KeyGen.Step[2] = UseChannelFund
	qc.KeyGen.Step[3] = msg.Peer() | 1<<31
	qc.KeyGen.Step[4] = cIdx | 1<<31
	qc.Value = amt
	//qc.Mode = portxo.TxoP2WSHComp
	qc.Op = op

	qc.TheirPub = msg.PubKey
	qc.TheirRefundPub = msg.RefundPub

	qc.MyPub, _ = nd.GetUsePub(qc.KeyGen, UseChannelFund)
	qc.MyRefundPub, _ = nd.GetUsePub(qc.KeyGen, UseChannelRefund)
	
	

	logging.Infof("got multisig output %s amt %d\n", op.String(), amt)

	// create initial state
	qc.State = new(StatCom)
	// similar to SIGREV in pushpull

	// TODO assumes both parties use same fee
	qc.State.Fee = wal.Fee() * consts.QcStateFee
	qc.State.MyAmt = msg.InitPayment

	qc.State.Data = msg.Data


	// save new channel to db
	err = nd.SaveQChan(qc)
	if err != nil {
		nd.FailChannel(qc)
		logging.Errorf("QChanDescHandler err %s", err.Error())
		return err
	}

	// load ... the thing I just saved.  why?
	qc, err = nd.GetQchan(opArr)
	if err != nil {
		nd.FailChannel(qc)
		logging.Errorf("QChanDescHandler GetQchan err %s", err.Error())
		return err
	}


	fmt.Printf("::%s:: QChanDescHandler(): qln/fund.go: nd.SignState(qc) \n",os.Args[6][len(os.Args[6])-4:])


	var sig [64]byte = [...]byte { 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0 }



	outMsg := lnutil.NewChanAckMsg(
		msg.Peer(), op,
		sig)
	outMsg.Bytes()

	nd.tmpSendLitMsg(outMsg)

	fmt.Printf("::%s:: QChanDescHandler() ----END----: qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])

	return nil
}

// FUNDER
// QChanAckHandler takes in an acknowledgement multisig description.
// when a multisig outpoint is ackd, that causes the funder to sign and broadcast.
func (nd *LitNode) QChanAckHandler(msg lnutil.ChanAckMsg, peer *RemotePeer) {
	opArr := lnutil.OutPointToBytes(msg.Outpoint)


	fmt.Printf("::%s:: QChanAckHandler() ----START----: qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])

	fmt.Printf("::%s:: opz QChanAckHandler() msg.Outpoint %+v \n",os.Args[6][len(os.Args[6])-4:], msg.Outpoint)

	// load channel to save their refund address
	qc, err := nd.GetQchan(opArr)
	if err != nil {
		nd.FailChannel(qc)
		logging.Errorf("QChanAckHandler GetQchan err %s", err.Error())
		return
	}



	// verify worked; Save state 1 to DB
	err = nd.SaveQchanState(qc)
	if err != nil {
		nd.FailChannel(qc)
		logging.Errorf("QChanAckHandler SaveQchanState err %s", err.Error())
		return
	}




	fmt.Printf("::%s:: QChanAckHandler(): qln/fund.go: nd.SignState(qc) \n",os.Args[6][len(os.Args[6])-4:])



	var sig [64]byte = [...]byte { 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0 }


	// OK to fund.
	err = nd.SubWallet[qc.Coin()].ReallySend(&qc.Op.Hash)
	if err != nil {
		nd.FailChannel(qc)
		logging.Errorf("QChanAckHandler ReallySend err %s", err.Error())
		return
	}




	nullTxo := new(portxo.PorTxo)
	nullTxo.Value = 0 
	nullTxo.KeyGen = qc.KeyGen
	nullTxo.KeyGen.Step[2] = UseChannelWatchRefund
	nd.SubWallet[qc.Coin()].ExportUtxo(nullTxo)



	nd.InProg.mtx.Lock()
	nd.InProg.done <- qc.KeyGen.Step[4] & 0x7fffffff
	nd.InProg.Clear()
	nd.InProg.mtx.Unlock()

	peer.QCs[qc.Idx()] = qc
	peer.OpMap[opArr] = qc.Idx()



	outMsg := lnutil.NewSigProofMsg(msg.Peer(), msg.Outpoint, sig)

	nd.tmpSendLitMsg(outMsg)

	fmt.Printf("::%s:: QChanAckHandler() ----END----: qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])

	return
}

// RECIPIENT

func (nd *LitNode) SigProofHandler(msg lnutil.SigProofMsg, peer *RemotePeer) {

	fmt.Printf("::%s:: SigProofHandler() ----START----: qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])

	op := msg.Outpoint
	opArr := lnutil.OutPointToBytes(op)

	qc, err := nd.GetQchan(opArr)
	if err != nil {
		nd.FailChannel(qc)
		logging.Errorf("SigProofHandler err %s", err.Error())
		return
	}

	wal, ok := nd.SubWallet[qc.Coin()]
	if !ok {
		nd.FailChannel(qc)
		logging.Errorf("Not connected to coin type %d\n", qc.Coin())
		return
	}



	// sig OK, save
	err = nd.SaveQchanState(qc)
	if err != nil {
		nd.FailChannel(qc)
		logging.Errorf("SigProofHandler err %s", err.Error())
		return
	}

	err = wal.WatchThis(op)

	if err != nil {
		nd.FailChannel(qc)
		logging.Errorf("SigProofHandler err %s", err.Error())
		return
	}

	// tell base wallet about watcher refund address in case that happens
	nullTxo := new(portxo.PorTxo)
	nullTxo.Value = 0 // redundant, but explicitly show that this is just for adr
	nullTxo.KeyGen = qc.KeyGen
	nullTxo.KeyGen.Step[2] = UseChannelWatchRefund
	wal.ExportUtxo(nullTxo)

	peer.QCs[qc.Idx()] = qc
	peer.OpMap[opArr] = qc.Idx()

	// sig OK; in terms of UI here's where you can say "payment received"
	// "channel online" etc

	peerIdx := qc.Peer()
	existingPeer := nd.PeerMan.GetPeerByIdx(int32(peerIdx))

	sigProofEvent := ChannelStateUpdateEvent{
		Action:   "sigproof",
		ChanIdx:  qc.Idx(),
		State:    qc.State,
		TheirPub: existingPeer.GetPubkey(),
		CoinType: qc.Coin(),
	}

	if succeed, err := nd.Events.Publish(sigProofEvent); err != nil {
		logging.Errorf("SigProofHandler publish err %s", err)
		return
	} else if !succeed {
		logging.Errorf("SigProofHandler publish did not succeed")
		return
	}

	fmt.Printf("::%s:: SigProofHandler() ----END----: qln/fund.go \n",os.Args[6][len(os.Args[6])-4:])

	return
}
