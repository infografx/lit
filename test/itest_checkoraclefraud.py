
def run_test(env):

    print('The oracle publish two different results')

    lit = env.lits[0]

    # s1string = "424b62134ec5ff7f8ff25de43917a03582e253283575b8f815d26bbdc27d17f8"
    # h1string = "9bd6e409476804596c2793eb722fd23479f2a1a8a439e8cb47faed68dc660535"
    # s2string = "4a61ef074997f0f0039c71e9dd91d15263c6f98bc54034336491d5e8a5445f4c"
    # h2string = "dc2b6ce71bb4099ca53c70eadcd1d9d4be46b65c1e0b540528e619fd236ae09a"

    # rpoint = "02f8460e855b091cec11ccf4a85064d4a8a7d3a2970b957a2165564b537d510bb4"  
    # apoint = "029bc17aed9a0a5821b5b0425d8260d66f0529eb357a0b036765d68904152f618a"

    # res = lit.rpc.DifferentResultsFraud(Sfirst=s1string, Hfirst=h1string, Ssecond=s2string, Hsecond=h2string, Rpoint=rpoint, Apoint=apoint)
    #print(res)

    #(CIdx=contract["Contract"]["Idx"])

    tx = "02000000000101865dbaa4dccd2d7bef0ac1a8d9fd91714b039c68e1174d227993974a007a672c0000000000ffffffff02608c120100000000220020488940ae6d80809b931f57901ee4a8d2ac98d0d61a2009c8ac130c513172270160681e000000000016001476aee327ba1f95dcad9c757f37f2fb3a9952a5760400483045022100ad7fb8902aa49daf3c91cd20384b41fa3ace4a1fb2a6eef8890951f71a54bf9d02201f10fa6e0c0f14e1fc68f66e99a30f2f320ae75a30349d11939ade9c9883cbc3014730440220421183ecb29cbd02608935c61fee0bc1e03978c5e3e23bba4dcf04db6297e8bb02203fb74283c602ce58349130769fa541b52e2db03ce72a54f478a0ad62ceed7e790147522102f7721a8167d3a7bcc98238460927d65181e2104760acc73840af75bf3ca321db210237f6e97101acac1594b74929bca29664914861bf1325543537ce44f8d723be5052ae00000000"

    res = lit.rpc.OracleCounterpartyFraud(CIdx=1, Tx=tx)
    print(res)