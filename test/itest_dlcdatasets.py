import testlib

import time, datetime
import json

import pprint

import requests # pip3 install requests

import codecs

deb_mod = False

def run_t(env, params):
    global deb_mod
    try:

        lit_funding_amt = params[0]
        contract_funding_amt = params[1]
        oracles_number = params[2]
        oracle_value = params[3]
        node_to_settle = params[4]
        valueFullyOurs=params[5]
        valueFullyTheirs=params[6]

        FundingTxVsize = params[7][0]
        SettlementTxVsize = params[7][1]

        feeperbyte = params[8]

        SetTxFeeOurs = params[9]
        SetTxFeeTheirs = params[10]

        ClaimTxFeeOurs = params[11]
        ClaimTxFeeTheirs = params[12]

        bc = env.bitcoind


        #------------
        # Create lits
        #------------

        lit1 = env.lits[0]
        lit2 = env.lits[1]

        #------------
        # Funding
        #------------



        pp = pprint.PrettyPrinter(indent=4)            


        lit1.connect_to_peer(lit2)

        addr1 = lit1.make_new_addr()
        txid1 = bc.rpc.sendtoaddress(addr1, lit_funding_amt)

        time.sleep(3)

        addr2 = lit2.make_new_addr()
        txid2 = bc.rpc.sendtoaddress(addr2, lit_funding_amt)

        time.sleep(3)

        env.generate_block()
        time.sleep(3)

        lit_funding_amt *= 100000000 




        #------------
        # Create oracles in test framework
        #------------

        oracles = []

        env.new_oracle(1, oracle_value)
        oracles.append(env.oracles[0])

        env.new_oracle(1, oracle_value)
        oracles.append(env.oracles[1])

        time.sleep(3)

        # # just print datasources
        for oracle in env.oracles:
            dss = json.loads(oracle.get_datasources())
            for ds in dss:
                print(ds)
                print('--------')
            print("==============")
 

        #------------
        # Add oracles
        #------------

        oracles_pubkey = []
        oidxs = []
        datasources = []

        opk = json.loads(oracles[0].get_pubkey())
        oracles_pubkey.append(opk)
        oidx = lit1.rpc.AddOracle(Key=opk["A"], Name=opk["A"])["Oracle"]["Idx"]
        lit2.rpc.AddOracle(Key=opk["A"], Name=opk["A"])["Oracle"]["Idx"]
        oidxs.append(oidx)

        print("opk['A']: ", opk["A"])

        opk = json.loads(oracles[1].get_pubkey())
        oracles_pubkey.append(opk)
        oidx = lit1.rpc.AddOracle(Key=opk["A"], Name=opk["A"])["Oracle"]["Idx"]
        lit2.rpc.AddOracle(Key=opk["A"], Name=opk["A"])["Oracle"]["Idx"]
        oidxs.append(oidx)

        print("opk['A']: ", opk["A"])

        #opk = json.loads(oracles[1].get_pubkey()) # here we use the same oracle but with different datasource
        oracles_pubkey.append(opk)
        oidx = lit1.rpc.AddOracle(Key=opk["A"], Name=opk["A"]+"_event")["Oracle"]["Idx"]
        lit2.rpc.AddOracle(Key=opk["A"], Name=opk["A"]+"_event")["Oracle"]["Idx"]
        oidxs.append(oidx)

        print("opk['A']: ", opk["A"])


        #------------
        # Now we have to create a contract in the lit1 node.
        #------------

        contract = lit1.rpc.NewContract()

        res = lit1.rpc.ListContracts()
        assert len(res["Contracts"]) == 1, "ListContracts does not works"


        res = lit1.rpc.GetContract(Idx=1)
        assert res["Contract"]["Idx"] == 1, "GetContract does not works"

 
        res = lit1.rpc.SetContractOraclesNumber(CIdx=contract["Contract"]["Idx"], OraclesNumber=3)
        assert res["Success"], "SetContractOraclesNumber does not works"


        res = lit1.rpc.SetContractOracle(CIdx=contract["Contract"]["Idx"], OIdx=[1,2,2])
        assert res["Success"], "SetContractOracle does not works"


        settlement_time = int(time.time()) + 3

        # dlc contract settime
        res = lit1.rpc.SetContractSettlementTime(CIdx=contract["Contract"]["Idx"], Time=settlement_time)
        assert res["Success"], "SetContractSettlementTime does not works"

        # we set settlement_time equal to refundtime, actually the refund transaction will be valid.
        res = lit1.rpc.SetContractRefundTime(CIdx=contract["Contract"]["Idx"], Time=settlement_time)
        assert res["Success"], "SetContractRefundTime does not works"

        res = lit1.rpc.ListContracts()
        assert res["Contracts"][contract["Contract"]["Idx"] - 1]["OracleTimestamp"] == settlement_time, "SetContractSettlementTime does not match settlement_time"


        #------------------
        # Add Rpoints
        #------------------

        dsTypePrice = 1
        dsTypeEvent = 2

        decode_hex = codecs.getdecoder("hex_codec")
        brpoints = []
        rpoints = []
        dstypes = []   

        res = oracles[0].get_rpoint(1, settlement_time)
        b_RPoint = decode_hex(json.loads(res)['R'])[0]
        RPoint = [elem for elem in b_RPoint]
        brpoints.append(RPoint)
        rpoints.append(res)
        dstypes.append(dsTypePrice)

        res = oracles[1].get_rpoint(1, settlement_time)
        b_RPoint = decode_hex(json.loads(res)['R'])[0]
        RPoint = [elem for elem in b_RPoint]
        brpoints.append(RPoint)
        rpoints.append(res) 
        dstypes.append(dsTypePrice)       


        res = oracles[1].get_eventrpoint("66e7a20e71a1585e1e467bac2c68e87a56ac73f0c8b19c7d8fead37185b6a192")
        b_RPoint = decode_hex(json.loads(res)['R'])[0]
        RPoint = [elem for elem in b_RPoint]
        brpoints.append(RPoint)
        rpoints.append(res)  
        dstypes.append(dsTypeEvent)


        res = lit1.rpc.SetContractRPoint(CIdx=contract["Contract"]["Idx"], RPoint=brpoints, DsType=dstypes)
        assert res["Success"], "SetContractRpoint does not works"


        lit1.rpc.SetContractCoinType(CIdx=contract["Contract"]["Idx"], CoinType = 257)
        res = lit1.rpc.GetContract(Idx=contract["Contract"]["Idx"])
        assert res["Contract"]["CoinType"] == 257, "SetContractCoinType does not works"


        lit1.rpc.SetContractFeePerByte(CIdx=contract["Contract"]["Idx"], FeePerByte = feeperbyte)
        res = lit1.rpc.GetContract(Idx=contract["Contract"]["Idx"])
        assert res["Contract"]["FeePerByte"] == feeperbyte, "SetContractFeePerByte does not works"  


        ourFundingAmount = contract_funding_amt
        theirFundingAmount = contract_funding_amt

        lit1.rpc.SetContractFunding(CIdx=contract["Contract"]["Idx"], OurAmount=ourFundingAmount, TheirAmount=theirFundingAmount)
        res = lit1.rpc.GetContract(Idx=contract["Contract"]["Idx"])
        assert res["Contract"]["OurFundingAmount"] == ourFundingAmount, "SetContractFunding does not works"
        assert res["Contract"]["TheirFundingAmount"] == theirFundingAmount, "SetContractFunding does not works"

        res = lit1.rpc.SetContractDivision(CIdx=contract["Contract"]["Idx"], ValueFullyOurs=valueFullyOurs, ValueFullyTheirs=valueFullyTheirs)
        assert res["Success"], "SetContractDivision does not works"
        
        time.sleep(5)


        res = lit1.rpc.ListConnections()

        res = lit1.rpc.OfferContract(CIdx=contract["Contract"]["Idx"], PeerIdx=lit1.get_peer_id(lit2))
        assert res["Success"], "OfferContract does not works"

        time.sleep(5)

        res = lit2.rpc.ContractRespond(AcceptOrDecline=True, CIdx=1)
        assert res["Success"], "ContractRespond on lit2 does not works"

        time.sleep(5)



        env.generate_block()
        time.sleep(2)

        print("Accept")
        bals1 = lit1.get_balance_info()  
        print('new lit1 balance:', bals1['TxoTotal'], 'in txos,', bals1['ChanTotal'], 'in chans')
        bal1sum = bals1['TxoTotal'] + bals1['ChanTotal']
        print('  = sum ', bal1sum)

        lit1_bal_after_accept = (lit_funding_amt - ourFundingAmount) - (126*feeperbyte)
        

        bals2 = lit2.get_balance_info()
        print('new lit2 balance:', bals2['TxoTotal'], 'in txos,', bals2['ChanTotal'], 'in chans')
        bal2sum = bals2['TxoTotal'] + bals2['ChanTotal']
        print('  = sum ', bal2sum)   

        lit2_bal_after_accept = (lit_funding_amt - theirFundingAmount) - (126*feeperbyte)


        assert bal1sum == lit1_bal_after_accept, "lit1 Balance after contract accept does not match"
        assert bal2sum == lit2_bal_after_accept, "lit2 Balance after contract accept does not match"        


        OraclesSig = []
        OraclesVal = []

        i = 0
        while True:

            publications_result = []

            for o, r in zip([oracles[0], oracles[1], oracles[1]], rpoints):
                publications_result.append(o.get_publication(json.loads(r)['R']))


            time.sleep(5)
            i += 1
            if i>4:
                assert False, "Error: Oracle does not publish data"
            
            try:

                for pr in publications_result:
                    oracle_val = json.loads(pr)["value"]
                    OraclesVal.append(oracle_val)
                    oracle_sig = json.loads(pr)["signature"]
                    b_OracleSig = decode_hex(oracle_sig)[0]
                    OracleSig = [elem for elem in b_OracleSig]
                    OraclesSig.append(OracleSig)                        

                break
            except BaseException as e:
                print(e)
                next

        # # Oracles have to publish the same value
        # vEqual = True
        # nTemp = OraclesVal[0]
        # for v in OraclesVal:
        #     if nTemp != v:
        #         vEqual = False
        #         break;
        # assert vEqual, "Oracles publish different values"      

        res = env.lits[node_to_settle].rpc.SettleContract(CIdx=contract["Contract"]["Idx"], OracleValue=OraclesVal[0], OracleSig=OraclesSig)
        assert res["Success"], "SettleContract does not works."


        time.sleep(3)

        try:
            env.generate_block(1)
            time.sleep(1)
            env.generate_block(1)
            time.sleep(1)
            env.generate_block(1)
            time.sleep(1)
        except BaseException as be:
            print("Exception After SettleContract: ")
            print(be)    


        bals1 = lit1.get_balance_info()  
        print('new lit1 balance:', bals1['TxoTotal'], 'in txos,', bals1['ChanTotal'], 'in chans')
        bal1sum = bals1['TxoTotal'] + bals1['ChanTotal']
        print(bals1)
        print('  = sum ', bal1sum)
        

        bals2 = lit2.get_balance_info()
        print('new lit2 balance:', bals2['TxoTotal'], 'in txos,', bals2['ChanTotal'], 'in chans')
        bal2sum = bals2['TxoTotal'] + bals2['ChanTotal']
        print(bals2)
        print('  = sum ', bal2sum)














    except BaseException as be:
        raise be  



def t_11_0(env):

    oracles_number = 2
    oracle_value = 11
    node_to_settle = 0

    valueFullyOurs=10
    valueFullyTheirs=20

    lit_funding_amt =      1     # 1 BTC
    contract_funding_amt = 10000000     # satoshi

    FundingTxVsize = 252
    SettlementTxVsize = 180

    SetTxFeeOurs = 7200
    SetTxFeeTheirs = 7200

    ClaimTxFeeOurs = 121 * 80
    ClaimTxFeeTheirs = 110 * 80


    feeperbyte = 80


    vsizes = [FundingTxVsize, SettlementTxVsize]

    params = [lit_funding_amt, contract_funding_amt, oracles_number, oracle_value, node_to_settle, valueFullyOurs, valueFullyTheirs, vsizes, feeperbyte, SetTxFeeOurs, SetTxFeeTheirs, ClaimTxFeeOurs, ClaimTxFeeTheirs]

    run_t(env, params)

