package qln

import (
	"fmt"

	"os"

	"bytes"
	"bufio"

	"github.com/mit-dci/lit/btcutil"
	"github.com/mit-dci/lit/btcutil/txscript"
	"github.com/mit-dci/lit/btcutil/txsort"
	"github.com/mit-dci/lit/crypto/koblitz"
	"github.com/mit-dci/lit/dlc"
	"github.com/mit-dci/lit/lnutil"
	"github.com/mit-dci/lit/logging"
	"github.com/mit-dci/lit/portxo"
	"github.com/mit-dci/lit/sig64"
	"github.com/mit-dci/lit/wire"
)

func (nd *LitNode) AddContract() (*lnutil.DlcContract, error) {

	c, err := nd.DlcManager.AddContract()
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (nd *LitNode) OfferDlc(peerIdx uint32, cIdx uint64) error {
	c, err := nd.DlcManager.LoadContract(cIdx)
	if err != nil {
		return err
	}

	if c.Status != lnutil.ContractStatusDraft {
		return fmt.Errorf("You cannot offer a contract to someone that is not in draft stage")
	}

	if !nd.ConnectedToPeer(peerIdx) {
		return fmt.Errorf("You are not connected to peer %d, do that first", peerIdx)
	}

	var nullBytes [33]byte
	// Check if everything's set
	if c.OracleA == nullBytes {
		return fmt.Errorf("You need to set an oracle for the contract before offering it")
	}

	if c.OracleR == nullBytes {
		return fmt.Errorf("You need to set an R-point for the contract before offering it")
	}

	if c.OracleTimestamp == 0 {
		return fmt.Errorf("You need to set a settlement time for the contract before offering it")
	}

	if c.CoinType == dlc.COINTYPE_NOT_SET {
		return fmt.Errorf("You need to set a coin type for the contract before offering it")
	}

	if c.FeePerByte == dlc.FEEPERBYTE_NOT_SET {
		return fmt.Errorf("You need to set a fee per byte for the contract before offering it")
	}		

	if c.Division == nil {
		return fmt.Errorf("You need to set a payout division for the contract before offering it")
	}

	if c.OurFundingAmount+c.TheirFundingAmount == 0 {
		return fmt.Errorf("You need to set a funding amount for the peers in contract before offering it")
	}

	c.PeerIdx = peerIdx

	var kg portxo.KeyGen
	kg.Depth = 5
	kg.Step[0] = 44 | 1<<31
	kg.Step[1] = c.CoinType | 1<<31
	kg.Step[2] = UseContractFundMultisig
	kg.Step[3] = c.PeerIdx | 1<<31
	kg.Step[4] = uint32(c.Idx) | 1<<31

	fmt.Printf("::%s:: OfferDlc(): qc.OurFundMultisigPub, err = nd.GetUsePub(kg, UseContractFundMultisig) %d \n",os.Args[6][len(os.Args[6])-4:], UseContractFundMultisig)

	c.OurFundMultisigPub, err = nd.GetUsePub(kg, UseContractFundMultisig)
	if err != nil {
		return err
	}

	c.OurPayoutBase, err = nd.GetUsePub(kg, UseContractPayoutBase)
	if err != nil {
		return err
	}

    ourPayoutPKHKey, err := nd.GetUsePub(kg, UseContractPayoutPKH)
    if err != nil {
        logging.Errorf("Error while getting our payout pubkey: %s", err.Error())
        c.Status = lnutil.ContractStatusError
        nd.DlcManager.SaveContract(c)
        return err
	}

	copy(c.OurPayoutPKH[:], btcutil.Hash160(ourPayoutPKHKey[:]))	

	// Fund the contract
	err = nd.FundContract(c)
	if err != nil {
		return err
	}


	c.OurRevokePub, err = nd.GetUsePub(kg, UseContractRevoke)

	fmt.Printf("::%s:: OfferDlc(): qln/dlc.go: c.OurRevokePub %x \n",os.Args[6][len(os.Args[6])-4:], c.OurRevokePub)


	msg := lnutil.NewDlcOfferMsg(peerIdx, c)

	c.Status = lnutil.ContractStatusOfferedByMe
	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		return err
	}

	nd.tmpSendLitMsg(msg)

	return nil
}

func (nd *LitNode) DeclineDlc(cIdx uint64, reason uint8) error {
	c, err := nd.DlcManager.LoadContract(cIdx)
	if err != nil {
		return err
	}

	if c.Status != lnutil.ContractStatusOfferedToMe {
		return fmt.Errorf("You cannot decline a contract unless it is in the 'Offered/Awaiting reply' state")
	}

	if !nd.ConnectedToPeer(c.PeerIdx) {
		return fmt.Errorf("You are not connected to peer %d, do that first", c.PeerIdx)
	}

	msg := lnutil.NewDlcOfferDeclineMsg(c.PeerIdx, reason, c.TheirIdx)
	c.Status = lnutil.ContractStatusDeclined

	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		return err
	}

	nd.tmpSendLitMsg(msg)

	return nil
}

func (nd *LitNode) AcceptDlc(cIdx uint64) error {
	c, err := nd.DlcManager.LoadContract(cIdx)
	if err != nil {
		return err
	}

	if c.Status != lnutil.ContractStatusOfferedToMe {
		return fmt.Errorf("You cannot decline a contract unless it is in the 'Offered/Awaiting reply' state")
	}

	if !nd.ConnectedToPeer(c.PeerIdx) {
		return fmt.Errorf("You are not connected to peer %d, do that first", c.PeerIdx)
	}

	// Preconditions checked - Go execute the acceptance in a separate go routine
	// while returning the status back to the client
	go func(nd *LitNode, c *lnutil.DlcContract) {
		c.Status = lnutil.ContractStatusAccepting
		nd.DlcManager.SaveContract(c)

		// Fund the contract
		err = nd.FundContract(c)
		if err != nil {
			c.Status = lnutil.ContractStatusError
			nd.DlcManager.SaveContract(c)
			return
		}

		var kg portxo.KeyGen
		kg.Depth = 5
		kg.Step[0] = 44 | 1<<31
		kg.Step[1] = c.CoinType | 1<<31
		kg.Step[2] = UseContractFundMultisig
		kg.Step[3] = c.PeerIdx | 1<<31
		kg.Step[4] = uint32(c.Idx) | 1<<31

		fmt.Printf("::%s:: AcceptDlc(): c.OurFundMultisigPub, err = nd.GetUsePub(kg, UseContractFundMultisig) %d \n",os.Args[6][len(os.Args[6])-4:], UseContractFundMultisig)

		c.OurFundMultisigPub, err = nd.GetUsePub(kg, UseContractFundMultisig)
		if err != nil {
			logging.Errorf("Error while getting multisig pubkey: %s", err.Error())
			c.Status = lnutil.ContractStatusError
			nd.DlcManager.SaveContract(c)
			return
		}

		c.OurPayoutBase, err = nd.GetUsePub(kg, UseContractPayoutBase)
		if err != nil {
			logging.Errorf("Error while getting payoutbase: %s", err.Error())
			c.Status = lnutil.ContractStatusError
			nd.DlcManager.SaveContract(c)
			return
		}

		ourPayoutPKHKey, err := nd.GetUsePub(kg, UseContractPayoutPKH)
		if err != nil {
			logging.Errorf("Error while getting our payout pubkey: %s", err.Error())
			c.Status = lnutil.ContractStatusError
			nd.DlcManager.SaveContract(c)
			return
		}
		copy(c.OurPayoutPKH[:], btcutil.Hash160(ourPayoutPKHKey[:]))

		c.OurRevokePub, err = nd.GetUsePub(kg, UseContractRevoke)	

		// Now we can sign the division
		sigs, err := nd.SignSettlementDivisions(c)
		if err != nil {
			logging.Errorf("Error signing settlement divisions: %s", err.Error())
			c.Status = lnutil.ContractStatusError
			nd.DlcManager.SaveContract(c)
			return
		}


		//-----------------------------------------------


		fmt.Printf("::%s::AcceptDlc(): qln/dlc.go: Start to Build Revoke Tx \n",os.Args[6][len(os.Args[6])-4:])

		revoketx := wire.NewMsgTx()
		revoketx.Version = 2

		fmt.Printf("::%s:: AcceptDlc(): qln/dlc.go: c.FundingOutpoint for TxIn %+v \n",os.Args[6][len(os.Args[6])-4:], c.FundingOutpoint)
	
		revoketx.AddTxIn(wire.NewTxIn(&c.FundingOutpoint, nil, nil))

		//--------------------------------------------------

		myrevscript :=lnutil.DirectWPKHScript(c.OurRevokePub)
		fmt.Printf("::%s::AcceptDlc(): qln/dlc.go: DirectWPKHScript: myScript %x \n",os.Args[6][len(os.Args[6])-4:], myrevscript)

		myOutput := wire.NewTxOut(800000, myrevscript)
		revoketx.AddTxOut(myOutput)


		//--------------------------------------------------


		theirrevscript :=lnutil.DirectWPKHScript(c.TheirRevokePub)
		fmt.Printf("::%s::AcceptDlc(): qln/dlc.go: DirectWPKHScript: theirScript %x \n",os.Args[6][len(os.Args[6])-4:], theirrevscript)

		theirOutput := wire.NewTxOut(800000, theirrevscript)
		revoketx.AddTxOut(theirOutput)

		//--------------------------------------------------

		
		txsort.InPlaceSort(revoketx)

		//--------------------------------------------------


		fmt.Printf("::%s:: AcceptDlc(): qln/dlc.go: lnutil.TxToString(theirtx) %s \n",os.Args[6][len(os.Args[6])-4:], lnutil.TxToString(revoketx))

		hCache := txscript.NewTxSigHashes(revoketx)
		
		revokepre, _, err := lnutil.FundTxScript(c.OurFundMultisigPub, c.TheirFundMultisigPub)

		kg.Step[2] = UseContractFundMultisig

		fmt.Printf("::%s:: AcceptDlc() priv, err := wal.GetPriv(kg) Step[2] %d \n",os.Args[6][len(os.Args[6])-4:], UseContractFundMultisig)
		

		wal, _ := nd.SubWallet[c.CoinType]
		priv, err := wal.GetPriv(kg)

		fmt.Printf("::%s:: AcceptDlc() lnutil.TxToString(revoketx) %x \n",os.Args[6][len(os.Args[6])-4:], lnutil.TxToString(revoketx))

		revoketxSig, err := txscript.RawTxInWitnessSignature(revoketx, hCache, 0, c.TheirFundingAmount+c.OurFundingAmount, revokepre, txscript.SigHashAll, priv)

		revoketxSig = revoketxSig[:len(revoketxSig)-1]

		revoketxSig64 , _ := sig64.SigCompress(revoketxSig)

		c.OurrevoketxSig64 = revoketxSig64

		fmt.Printf("::%s::AcceptDlc(): qln/dlc.go: c.OurRevokePub %x, c.TheirRevokePub %x, c.OurrevoketxSig64 %x \n",os.Args[6][len(os.Args[6])-4:], c.OurRevokePub, c.TheirRevokePub, c.OurrevoketxSig64)

		//------------------------------------------------		

		msg := lnutil.NewDlcOfferAcceptMsg(c, sigs)
		c.Status = lnutil.ContractStatusAccepted

		nd.DlcManager.SaveContract(c)
		nd.tmpSendLitMsg(msg)
	}(nd, c)
	return nil
}

func (nd *LitNode) DlcOfferHandler(msg lnutil.DlcOfferMsg, peer *RemotePeer) {
	c := new(lnutil.DlcContract)

	c.PeerIdx = peer.Idx
	c.Status = lnutil.ContractStatusOfferedToMe
	// Reverse copy from the contract we received
	c.OurFundingAmount = msg.Contract.TheirFundingAmount
	c.TheirFundingAmount = msg.Contract.OurFundingAmount
	c.OurFundingInputs = msg.Contract.TheirFundingInputs
	c.TheirFundingInputs = msg.Contract.OurFundingInputs
	c.OurFundMultisigPub = msg.Contract.TheirFundMultisigPub
	c.TheirFundMultisigPub = msg.Contract.OurFundMultisigPub
	c.OurPayoutBase = msg.Contract.TheirPayoutBase
	c.TheirPayoutBase = msg.Contract.OurPayoutBase
	c.OurChangePKH = msg.Contract.TheirChangePKH
	c.TheirChangePKH = msg.Contract.OurChangePKH
	c.TheirIdx = msg.Contract.Idx
	c.TheirPayoutPKH = msg.Contract.OurPayoutPKH

	c.TheirRevokePub = msg.Contract.OurRevokePub

	fmt.Printf("::%s:: DlcOfferHandler(): qln/dlc.go: c.TheirRevokePub %x \n",os.Args[6][len(os.Args[6])-4:], c.TheirRevokePub)

	fmt.Printf("::%s:: DlcOfferHandler(): qln/dlc.go: msg.Contract %+v \n",os.Args[6][len(os.Args[6])-4:], msg.Contract)

	c.Division = make([]lnutil.DlcContractDivision, len(msg.Contract.Division))
	for i := 0; i < len(msg.Contract.Division); i++ {
		c.Division[i].OracleValue = msg.Contract.Division[i].OracleValue
		c.Division[i].ValueOurs = (c.TheirFundingAmount + c.OurFundingAmount) - msg.Contract.Division[i].ValueOurs
	}

	// Copy
	c.CoinType = msg.Contract.CoinType
	c.FeePerByte = msg.Contract.FeePerByte
	c.OracleA = msg.Contract.OracleA
	c.OracleR = msg.Contract.OracleR
	c.OracleTimestamp = msg.Contract.OracleTimestamp

	err := nd.DlcManager.SaveContract(c)
	if err != nil {
		logging.Errorf("DlcOfferHandler SaveContract err %s\n", err.Error())
		return
	}

	_, ok := nd.SubWallet[msg.Contract.CoinType]
	if !ok {
		// We don't have this coin type, automatically decline
		nd.DeclineDlc(c.Idx, 0x02)
	}

}

func (nd *LitNode) DlcDeclineHandler(msg lnutil.DlcOfferDeclineMsg, peer *RemotePeer) {
	c, err := nd.DlcManager.LoadContract(msg.Idx)
	if err != nil {
		logging.Errorf("DlcDeclineHandler FindContract err %s\n", err.Error())
		return
	}

	c.Status = lnutil.ContractStatusDeclined
	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		logging.Errorf("DlcDeclineHandler SaveContract err %s\n", err.Error())
		return
	}
}

func (nd *LitNode) DlcAcceptHandler(msg lnutil.DlcOfferAcceptMsg, peer *RemotePeer) error {
	c, err := nd.DlcManager.LoadContract(msg.Idx)
	if err != nil {
		logging.Errorf("DlcAcceptHandler FindContract err %s\n", err.Error())
		return err
	}

	// TODO: Check signatures

	c.TheirChangePKH = msg.OurChangePKH
	c.TheirFundingInputs = msg.FundingInputs
	c.TheirSettlementSignatures = msg.SettlementSignatures
	c.TheirFundMultisigPub = msg.OurFundMultisigPub
	c.TheirPayoutBase = msg.OurPayoutBase
	c.TheirPayoutPKH = msg.OurPayoutPKH
	c.TheirIdx = msg.OurIdx

	c.TheirRevokePub = msg.OurRevokePub

	fmt.Printf("::%s:: DlcAcceptHandler(): qln/dlc.go: msg.OurrevoketxSig64: %x \n",os.Args[6][len(os.Args[6])-4:], msg.OurrevoketxSig64)

	c.TheirrevoketxSig64 = msg.OurrevoketxSig64

	c.Status = lnutil.ContractStatusAccepted
	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		logging.Errorf("DlcAcceptHandler SaveContract err %s\n", err.Error())
		return err
	}

	// create our settlement signatures and ack
	sigs, err := nd.SignSettlementDivisions(c)
	if err != nil {
		return err
	}

	outMsg := lnutil.NewDlcContractAckMsg(c, sigs)
	c.Status = lnutil.ContractStatusAcknowledged

	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		return err
	}

	nd.tmpSendLitMsg(outMsg)

	return nil

}

func (nd *LitNode) DlcContractAckHandler(msg lnutil.DlcContractAckMsg, peer *RemotePeer) {
	c, err := nd.DlcManager.LoadContract(msg.Idx)
	if err != nil {
		logging.Errorf("DlcContractAckHandler FindContract err %s\n", err.Error())
		return
	}

	// TODO: Check signatures

	c.Status = lnutil.ContractStatusAcknowledged

	c.TheirSettlementSignatures = msg.SettlementSignatures

	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		logging.Errorf("DlcContractAckHandler SaveContract err %s\n", err.Error())
		return
	}

	// We have everything now, send our signatures to the funding TX
	wal, ok := nd.SubWallet[c.CoinType]
	if !ok {
		logging.Errorf("DlcContractAckHandler No wallet for cointype %d\n", c.CoinType)
		return
	}


	fmt.Printf("::%s:: DlcContractAckHandler(): BuildDlcFundingTransaction(): \n",os.Args[6][len(os.Args[6])-4:])
	tx, err := nd.BuildDlcFundingTransaction(c)
	if err != nil {
		logging.Errorf("DlcContractAckHandler BuildDlcFundingTransaction err %s\n", err.Error())
		return
	}

	err = wal.SignMyInputs(&tx)
	if err != nil {
		logging.Errorf("DlcContractAckHandler SignMyInputs err %s\n", err.Error())
		return
	}

	outMsg := lnutil.NewDlcContractFundingSigsMsg(c, &tx)

	nd.tmpSendLitMsg(outMsg)
}

func (nd *LitNode) DlcFundingSigsHandler(msg lnutil.DlcContractFundingSigsMsg, peer *RemotePeer) {
	c, err := nd.DlcManager.LoadContract(msg.Idx)
	if err != nil {
		logging.Errorf("DlcFundingSigsHandler FindContract err %s\n", err.Error())
		return
	}

	// TODO: Check signatures

	// We have everything now. Sign our inputs to the funding TX and send it to the blockchain.
	wal, ok := nd.SubWallet[c.CoinType]
	if !ok {
		logging.Errorf("DlcFundingSigsHandler No wallet for cointype %d\n", c.CoinType)
		return
	}

	wal.SignMyInputs(msg.SignedFundingTx)


	fmt.Printf("::%s:: DlcFundingSigsHandler(): qln/dlc.go: lnutil.TxToString(msg.SignedFundingTx) %s \n",os.Args[6][len(os.Args[6])-4:], lnutil.TxToString(msg.SignedFundingTx))


	var buft bytes.Buffer
	wtt := bufio.NewWriter(&buft)
	msg.SignedFundingTx.Serialize(wtt)
	wtt.Flush()


	fmt.Printf("::%s:: DlcFundingSigsHandler(): qln/dlc.go: CONTRACT %+v \n",os.Args[6][len(os.Args[6])-4:], c)


	fmt.Printf("::%s:: DlcFundingSigsHandler(): qln/dlc.go: msg.SignedFundingTx %x \n",os.Args[6][len(os.Args[6])-4:], buft.Bytes())





	wal.DirectSendTx(msg.SignedFundingTx)

	err = wal.WatchThis(c.FundingOutpoint)
	if err != nil {
		logging.Errorf("DlcFundingSigsHandler WatchThis err %s\n", err.Error())
		return
	}

	c.Status = lnutil.ContractStatusActive
	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		logging.Errorf("DlcFundingSigsHandler SaveContract err %s\n", err.Error())
		return
	}

	outMsg := lnutil.NewDlcContractSigProofMsg(c, msg.SignedFundingTx)

	nd.tmpSendLitMsg(outMsg)
}

func (nd *LitNode) DlcSigProofHandler(msg lnutil.DlcContractSigProofMsg, peer *RemotePeer) {
	c, err := nd.DlcManager.LoadContract(msg.Idx)
	if err != nil {
		logging.Errorf("DlcSigProofHandler FindContract err %s\n", err.Error())
		return
	}

	// TODO: Check signatures
	wal, ok := nd.SubWallet[c.CoinType]
	if !ok {
		logging.Errorf("DlcSigProofHandler No wallet for cointype %d\n", c.CoinType)
		return
	}

	err = wal.WatchThis(c.FundingOutpoint)
	if err != nil {
		logging.Errorf("DlcSigProofHandler WatchThis err %s\n", err.Error())
		return
	}

	c.Status = lnutil.ContractStatusActive
	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		logging.Errorf("DlcSigProofHandler SaveContract err %s\n", err.Error())
		return
	}
}

func (nd *LitNode) SignSettlementDivisions(c *lnutil.DlcContract) ([]lnutil.DlcContractSettlementSignature, error) {
	wal, ok := nd.SubWallet[c.CoinType]
	if !ok {
		return nil, fmt.Errorf("Wallet of type %d not found", c.CoinType)
	}

	var kg portxo.KeyGen
	kg.Depth = 5
	kg.Step[0] = 44 | 1<<31
	kg.Step[1] = c.CoinType | 1<<31
	kg.Step[2] = UseContractFundMultisig
	kg.Step[3] = c.PeerIdx | 1<<31
	kg.Step[4] = uint32(c.Idx) | 1<<31

	fmt.Printf("::%s:: SignSettlementDivisions(): priv, err := wal.GetPriv(kg) %d \n",os.Args[6][len(os.Args[6])-4:], UseContractFundMultisig)

	priv, err := wal.GetPriv(kg)
	if err != nil {
		return nil, fmt.Errorf("Could not get private key for contract %d", c.Idx)
	}


	fmt.Printf("::%s:: SignSettlementDivisions(): BuildDlcFundingTransaction() \n",os.Args[6][len(os.Args[6])-4:])

	fundingTx, err := nd.BuildDlcFundingTransaction(c)
	if err != nil {
		return nil, err
	}

	c.FundingOutpoint = wire.OutPoint{Hash: fundingTx.TxHash(), Index: 0}

	returnValue := make([]lnutil.DlcContractSettlementSignature, len(c.Division))
	for i, d := range c.Division {
		tx, err := lnutil.SettlementTx(c, d, true)
		if err != nil {
			return nil, err
		}
		sig, err := nd.SignSettlementTx(c, tx, priv)
		if err != nil {
			return nil, err
		}
		returnValue[i].Outcome = d.OracleValue
		returnValue[i].Signature = sig
	}

	return returnValue, nil
}

func (nd *LitNode) BuildDlcFundingTransaction(c *lnutil.DlcContract) (wire.MsgTx, error) {
	// make the tx
	tx := wire.NewMsgTx()

	// set version 2, for op_csv
	tx.Version = 2

	// add all the txins
	var ourInputTotal int64
	var theirInputTotal int64

	our_txin_num := 0
	for _, u := range c.OurFundingInputs {
		txin := wire.NewTxIn(&u.Outpoint, nil, nil)

		fmt.Printf("::%s:: BuildDlcFundingTransaction()1: wallit/signsend.go: c.OurFundingInputs: AddTxIn: u.Outpoint.Index %d, u.Outpoint.Hash %x \n",os.Args[6][len(os.Args[6])-4:], u.Outpoint.Index, u.Outpoint.Hash)


		tx.AddTxIn(txin)
		ourInputTotal += u.Value
		our_txin_num += 1

	}


	their_txin_num := 0
	for _, u := range c.TheirFundingInputs {
		txin := wire.NewTxIn(&u.Outpoint, nil, nil)

		fmt.Printf("::%s:: BuildDlcFundingTransaction()2: wallit/signsend.go: c.TheirFundingInputs: AddTxIn: u.Outpoint.Index %d, u.Outpoint.Hash %x \n",os.Args[6][len(os.Args[6])-4:], u.Outpoint.Index, u.Outpoint.Hash)

		tx.AddTxIn(txin)
		theirInputTotal += u.Value
		their_txin_num += 1

	}



	//====================================================

	// Here can be a situation when peers have different number of inputs.
	// Therefore we have to calculate fees for each peer separately.

	// This transaction always will have 3 outputs ( 43 + 31 + 31)
	tx_basesize := 10 + 43 + 31 + 31
	tx_size_foreach := tx_basesize / 2
	tx_size_foreach += 1 // rounding

	input_wit_size := 107

	our_tx_vsize := uint32(((tx_size_foreach + (41 * our_txin_num)) * 3 + (tx_size_foreach + (41 * our_txin_num) + (input_wit_size*our_txin_num) )) / 4)
	their_tx_vsize := uint32(((tx_size_foreach + (41 * their_txin_num)) * 3 + (tx_size_foreach + (41 * their_txin_num) + (input_wit_size*their_txin_num) )) / 4)

	//rounding
	our_tx_vsize += 1
	their_tx_vsize += 1


	our_fee := int64(our_tx_vsize * c.FeePerByte)
	their_fee := int64(their_tx_vsize * c.FeePerByte)

	// add change and sort

	their_txout := wire.NewTxOut(theirInputTotal-c.TheirFundingAmount-their_fee, lnutil.DirectWPKHScriptFromPKH(c.TheirChangePKH)) 
	tx.AddTxOut(their_txout)

	fmt.Printf("::%s:: BuildDlcFundingTransaction()3: wallit/signsend.go: AddTxOut: their_txout.Value %d, their_txout.PkScript %x \n",os.Args[6][len(os.Args[6])-4:], their_txout.Value, their_txout.PkScript)

	our_txout := wire.NewTxOut(ourInputTotal-c.OurFundingAmount-our_fee, lnutil.DirectWPKHScriptFromPKH(c.OurChangePKH))
	tx.AddTxOut(our_txout)

	fmt.Printf("::%s:: BuildDlcFundingTransaction()4: wallit/signsend.go: AddTxOut: our_txout.Value %d, our_txout.PkScript %x \n",os.Args[6][len(os.Args[6])-4:], our_txout.Value, our_txout.PkScript)

	

	txsort.InPlaceSort(tx)

	// get txo for channel
	txo, err := lnutil.FundTxOut(c.TheirFundMultisigPub, c.OurFundMultisigPub, c.OurFundingAmount+c.TheirFundingAmount)
	if err != nil {
		return *tx, err
	}

	fmt.Printf("::%s:: BuildDlcFundingTransaction()5: wallit/signsend.go: AddTxOut txo.PkScript %x \n",os.Args[6][len(os.Args[6])-4:], txo.PkScript)

	// Ensure contract funding output is always at position 0
	txos := make([]*wire.TxOut, len(tx.TxOut)+1)
	txos[0] = txo
	copy(txos[1:], tx.TxOut)
	tx.TxOut = txos

	return *tx, nil

}

func (nd *LitNode) FundContract(c *lnutil.DlcContract) error {
	wal, ok := nd.SubWallet[c.CoinType]
	if !ok {
		return fmt.Errorf("No wallet of type %d connected", c.CoinType)
	}

	utxos, _, err := wal.PickUtxos(int64(c.OurFundingAmount), 500, wal.Fee(), true)
	if err != nil {
		return err
	}

	c.OurFundingInputs = make([]lnutil.DlcContractFundingInput, len(utxos))
	for i := 0; i < len(utxos); i++ {
		c.OurFundingInputs[i] = lnutil.DlcContractFundingInput{Outpoint: utxos[i].Op, Value: utxos[i].Value}
	}

	c.OurChangePKH, err = wal.NewAdr()
	if err != nil {
		return err
	}

	return nil
}

func (nd *LitNode) SettleContract(cIdx uint64, oracleValue int64, oracleSig [32]byte) ([32]byte, [32]byte, error) {

	c, err := nd.DlcManager.LoadContract(cIdx)
	if err != nil {
		logging.Errorf("SettleContract FindContract err %s\n", err.Error())
		return [32]byte{}, [32]byte{}, err
	}

	c.Status = lnutil.ContractStatusSettling
	err = nd.DlcManager.SaveContract(c)
	if err != nil {
		logging.Errorf("SettleContract SaveContract err %s\n", err.Error())
		return [32]byte{}, [32]byte{}, err
	}

	d, err := c.GetDivision(oracleValue)
	if err != nil {
		logging.Errorf("SettleContract GetDivision err %s\n", err.Error())
		return [32]byte{}, [32]byte{}, err
	}

	wal, ok := nd.SubWallet[c.CoinType]
	if !ok {
		return [32]byte{}, [32]byte{}, fmt.Errorf("SettleContract Wallet of type %d not found", c.CoinType)
	}

	var kg portxo.KeyGen
	kg.Depth = 5
	kg.Step[0] = 44 | 1<<31
	kg.Step[1] = c.CoinType | 1<<31
	kg.Step[2] = UseContractFundMultisig
	kg.Step[3] = c.PeerIdx | 1<<31
	kg.Step[4] = uint32(c.Idx) | 1<<31

	fmt.Printf("::%s:: SettleContract(): priv, err := wal.GetPriv(kg) %d \n",os.Args[6][len(os.Args[6])-4:], UseContractFundMultisig)

	priv, err := wal.GetPriv(kg)
	if err != nil {
		return [32]byte{}, [32]byte{}, fmt.Errorf("SettleContract Could not get private key for contract %d", c.Idx)
	}

	settleTx, err := lnutil.SettlementTx(c, *d, false)
	if err != nil {
		logging.Errorf("SettleContract SettlementTx err %s\n", err.Error())
		return [32]byte{}, [32]byte{}, err
	}

	mySig, err := nd.SignSettlementTx(c, settleTx, priv)
	if err != nil {
		logging.Errorf("SettleContract SignSettlementTx err %s", err.Error())
		return [32]byte{}, [32]byte{}, err
	}

	myBigSig := sig64.SigDecompress(mySig)

	theirSig, err := c.GetTheirSettlementSignature(oracleValue)
	theirBigSig := sig64.SigDecompress(theirSig)

	// put the sighash all byte on the end of both signatures
	myBigSig = append(myBigSig, byte(txscript.SigHashAll))
	theirBigSig = append(theirBigSig, byte(txscript.SigHashAll))

	fmt.Printf("::%s:: FundTxScript(): SettleContract(): qln/dlc.go: c.OurFundMultisigPub %x, c.TheirFundMultisigPub %x \n",os.Args[6][len(os.Args[6])-4:], c.OurFundMultisigPub, c.TheirFundMultisigPub)

	pre, swap, err := lnutil.FundTxScript(c.OurFundMultisigPub, c.TheirFundMultisigPub)
	if err != nil {
		logging.Errorf("SettleContract FundTxScript err %s", err.Error())
		return [32]byte{}, [32]byte{}, err
	}
		

	// swap if needed
	if swap {
		settleTx.TxIn[0].Witness = SpendMultiSigWitStack(pre, theirBigSig, myBigSig)
	} else {
		settleTx.TxIn[0].Witness = SpendMultiSigWitStack(pre, myBigSig, theirBigSig)
	}

	// Settlement TX should be valid here, so publish it.
	err = wal.DirectSendTx(settleTx)
	if err != nil {
		logging.Errorf("SettleContract DirectSendTx (settle) err %s", err.Error())
		return [32]byte{}, [32]byte{}, err
	}


	//===========================================
	// Claim TX
	//===========================================


	// Here the transaction size is always the same
	// n := 8 + VarIntSerializeSize(uint64(len(msg.TxIn))) +
	// 	VarIntSerializeSize(uint64(len(msg.TxOut)))
	// n = 10
	// Plus Single input 41
	// Plus Single output 31
	// Plus 2 for all wittness transactions
	// Plus Witness Data 151

	// TxSize = 4 + 4 + 1 + 1 + 2 + 151 + 41 + 31 = 235
	// Vsize = ((235 - 151 - 2) * 3 + 235) / 4 = 120,25


	if ( d.ValueOurs != 0){

		vsize := uint32(121)
		fee := vsize * c.FeePerByte
	
		// TODO: Claim the contract settlement output back to our wallet - otherwise the peer can claim it after locktime.
		txClaim := wire.NewMsgTx()
		txClaim.Version = 2

		settleOutpoint := wire.OutPoint{Hash: settleTx.TxHash(), Index: 0}
		txClaim.AddTxIn(wire.NewTxIn(&settleOutpoint, nil, nil))

		addr, err := wal.NewAdr()
		txClaim.AddTxOut(wire.NewTxOut(settleTx.TxOut[0].Value-int64(fee), lnutil.DirectWPKHScriptFromPKH(addr)))

		kg.Step[2] = UseContractPayoutBase
		privSpend, _ := wal.GetPriv(kg)

		pubSpend := wal.GetPub(kg)
		privOracle, pubOracle := koblitz.PrivKeyFromBytes(koblitz.S256(), oracleSig[:])
		privContractOutput := lnutil.CombinePrivateKeys(privSpend, privOracle)

		var pubOracleBytes [33]byte
		copy(pubOracleBytes[:], pubOracle.SerializeCompressed())
		var pubSpendBytes [33]byte
		copy(pubSpendBytes[:], pubSpend.SerializeCompressed())

		settleScript := lnutil.DlcCommitScript(c.OurPayoutBase, pubOracleBytes, c.TheirPayoutBase, 5)
		err = nd.SignClaimTx(txClaim, settleTx.TxOut[0].Value, settleScript, privContractOutput, false)
		if err != nil {
			logging.Errorf("SettleContract SignClaimTx err %s", err.Error())
			return [32]byte{}, [32]byte{}, err
		}

		// Claim TX should be valid here, so publish it.
		err = wal.DirectSendTx(txClaim)
		if err != nil {
			logging.Errorf("SettleContract DirectSendTx (claim) err %s", err.Error())
			return [32]byte{}, [32]byte{}, err
		}

		c.Status = lnutil.ContractStatusClosed
		err = nd.DlcManager.SaveContract(c)
		if err != nil {
			return [32]byte{}, [32]byte{}, err
		}
		return settleTx.TxHash(), txClaim.TxHash(), nil

	}else{

		return settleTx.TxHash(), [32]byte{}, nil

	}

}



//======================================================================


func (nd *LitNode) RevokeContract(cIdx uint64) (bool, error) {
	
	fmt.Printf("::%s:: RevokeContract() ----START----: qln/dlc.go \n",os.Args[6][len(os.Args[6])-4:])


	c, err := nd.DlcManager.LoadContract(cIdx)
	if err != nil {
		logging.Errorf("SettleContract FindContract err %s\n", err.Error())
		return false, err
	}


	wal, _ := nd.SubWallet[c.CoinType]


	//----------------------------------------------------
	// MY Simple close tx START
	//----------------------------------------------------

	mytx := wire.NewMsgTx()
	// set version 2, for op_csv
	mytx.Version = 2

	fmt.Printf("::%s:: RevokeContract(): qln/dlc.go: c.FundingOutpoint for TxIn %+v \n",os.Args[6][len(os.Args[6])-4:], c.FundingOutpoint)

	mytx.AddTxIn(wire.NewTxIn(&c.FundingOutpoint, nil, nil))	


	myScript := lnutil.DirectWPKHScript(c.OurRevokePub)

	myOutput := wire.NewTxOut(800000, myScript)
	mytx.AddTxOut(myOutput)

	fmt.Printf("::%s::RevokeContract(): qln/dlc.go: DirectWPKHScript: myScript %x \n",os.Args[6][len(os.Args[6])-4:], myScript)


	theirScript := lnutil.DirectWPKHScript(c.TheirRevokePub)

	theirOutput := wire.NewTxOut(800000, theirScript)
	mytx.AddTxOut(theirOutput)

	fmt.Printf("::%s::RevokeContract(): qln/dlc.go: DirectWPKHScript: theirScript %x \n",os.Args[6][len(os.Args[6])-4:], theirScript)

	
	txsort.InPlaceSort(mytx)

	fmt.Printf("::%s::RevokeContract(): qln/dlc.go: lnutil.TxToString(mytx) %s \n",os.Args[6][len(os.Args[6])-4:], lnutil.TxToString(mytx))

	
	//----------------------------------------------------
	// MY Simple close tx END
	//----------------------------------------------------


	//----------------------------------------------------
	// MY Sign simple close tx START
	//----------------------------------------------------


	myhCache := txscript.NewTxSigHashes(mytx)



	mypre, _, err := lnutil.FundTxScript(c.OurFundMultisigPub, c.TheirFundMultisigPub)


	var kg portxo.KeyGen
	kg.Depth = 5
	kg.Step[0] = 44 | 1<<31
	kg.Step[1] = c.CoinType | 1<<31
	kg.Step[2] = UseContractFundMultisig
	kg.Step[3] = c.PeerIdx | 1<<31
	kg.Step[4] = uint32(c.Idx) | 1<<31

	fmt.Printf("::%s:: RevokeContract() mypriv, err := wal.GetPriv(kg) %d \n",os.Args[6][len(os.Args[6])-4:], UseContractFundMultisig)

	mypriv, err := wal.GetPriv(kg)


	fmt.Printf("::%s:: RevokeContract() lnutil.TxToString(mytx) %x \n",os.Args[6][len(os.Args[6])-4:], lnutil.TxToString(mytx))

	mySig, err := txscript.RawTxInWitnessSignature(mytx, myhCache, 0, c.TheirFundingAmount+c.OurFundingAmount, mypre, txscript.SigHashAll, mypriv)

	mySig = mySig[:len(mySig)-1]
	
	mySig64 , _ := sig64.SigCompress(mySig)

	//--------------------------------------------

	myBigSig := sig64.SigDecompress(mySig64)

	myBigSig = append(myBigSig, byte(txscript.SigHashAll))	


	//----------------------------------------------------
	// MY Sign simple close tx END
	//----------------------------------------------------

	fmt.Printf("::%s::RevokeContract(): qln/dlc.go: c.TheirrevoketxSig64 %x \n",os.Args[6][len(os.Args[6])-4:], c.TheirrevoketxSig64)

	theirBigSig := sig64.SigDecompress(c.TheirrevoketxSig64)

	theirBigSig = append(theirBigSig, byte(txscript.SigHashAll))


	// //--------------------------------------
	// //--------------------------------------
	// // Verify Their sig

	// pSig, err := koblitz.ParseDERSignature(theirBigSig, koblitz.S256())
	// if err != nil {
	// 	fmt.Printf("RevokeContract Their Sig err %s", err.Error())

	// }

	// theirPubKey, err := koblitz.ParsePubKey(c.TheirFundMultisigPub[:], koblitz.S256())
	// if err != nil {
	// 	fmt.Printf("RevokeContract Their PubKey err %s", err.Error())

	// }

	// theirpre, _, err := lnutil.FundTxScript(c.TheirFundMultisigPub, c.OurFundMultisigPub)

	// theirparsed, err := txscript.ParseScript(theirpre)
	// if err != nil {
	// 	fmt.Printf("RevokeContract ParseScript err %s", err.Error())
	// }


	// hash := txscript.CalcWitnessSignatureHash(
	// 	theirparsed, myhCache, txscript.SigHashAll, mytx, 0, 800000)


	// worked := pSig.Verify(hash, theirPubKey)
	// if !worked {
	// 	fmt.Printf("zzzzzz RevokeContract Their Sig err invalid signature on close tx %s", err.Error())

	// }else{
	// 	fmt.Println("Their Sig Worked")
	// }

	// //--------------------------------------
	// //--------------------------------------


	// pSig, err = koblitz.ParseDERSignature(myBigSig, koblitz.S256())
	// if err != nil {
	// 	fmt.Printf("RevokeContract My Sig err %s", err.Error())

	// }

	// myPubKey, err := koblitz.ParsePubKey(c.OurFundMultisigPub[:], koblitz.S256())
	// if err != nil {
	// 	fmt.Printf("RevokeContract My PubKey err %s", err.Error())

	// }


	// parsed, err := txscript.ParseScript(mypre)
	// if err != nil {
	// 	fmt.Printf("RevokeContract ParseScript err %s", err.Error())
	// }


	// hash = txscript.CalcWitnessSignatureHash(
	// 	parsed, myhCache, txscript.SigHashAll, mytx, 0, 800000)


	// worked = pSig.Verify(hash, myPubKey)
	// if !worked {
	// 	fmt.Printf("zzzzzz RevokeContract My Sig err invalid signature on close tx %s", err.Error())

	// }else{
	// 	fmt.Println("My Sig Worked")
	// }


	//--------------------------------------
	//--------------------------------------
	
	
	
	//=================================================================================


	fmt.Printf("::%s:: RevokeContract(): qln/dlc.go: myBigSig %x, theirBigSig %x \n",os.Args[6][len(os.Args[6])-4:], myBigSig, theirBigSig)

	pre, swap, err := lnutil.FundTxScript(c.OurFundMultisigPub, c.TheirFundMultisigPub)

	
	// swap if needed
	if swap {
		mytx.TxIn[0].Witness = SpendMultiSigWitStack(pre, theirBigSig, myBigSig)
	} else {
		mytx.TxIn[0].Witness = SpendMultiSigWitStack(pre, myBigSig, theirBigSig)
	}	


	fmt.Printf("::%s:: RevokeContract(): qln/dlc.go: lnutil.TxToString(mytx) with wit %s \n",os.Args[6][len(os.Args[6])-4:], lnutil.TxToString(mytx))


	var buft bytes.Buffer
	wtt := bufio.NewWriter(&buft)
	mytx.Serialize(wtt)
	wtt.Flush()


	fmt.Printf("::%s:: RevokeContract(): qln/dlc.go: mytx %x \n",os.Args[6][len(os.Args[6])-4:], buft.Bytes())


	
	err = wal.DirectSendTx(mytx)



	fmt.Printf("::%s:: RevokeContract(): qln/dlc.go: c: %+v \n", os.Args[6][len(os.Args[6])-4:], c)


	fmt.Printf("::%s:: RevokeContract() ----END----: qln/dlc.go \n",os.Args[6][len(os.Args[6])-4:])



	return true, nil

}
