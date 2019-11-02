
def run_test(env):

    print('The oracle publish two different results')

    lit = env.lits[0]

    # s1string = "ea924eacf7c83eefaf055e3a2f1f6278261dea2bef8fffc51aeb7b9cd61504fe"
    # h1string = "3f6b0797d46834e467b5e0fed7df7191fc61b98d1b02239d9ae7931dec8d6c63"
    # s2string = "9507d8170551261c5dd7c84fab00b7db62c38e7c8456066005a24f2e8ec46a09"
    # h2string = "eb8dba99034ac27bc66e715dc4d8f01f49ab06a57066ae4fa643fff228bc0edc"


    s1string = "424b62134ec5ff7f8ff25de43917a03582e253283575b8f815d26bbdc27d17f8"
    h1string = "9bd6e409476804596c2793eb722fd23479f2a1a8a439e8cb47faed68dc660535"
    s2string = "4a61ef074997f0f0039c71e9dd91d15263c6f98bc54034336491d5e8a5445f4c"
    h2string = "dc2b6ce71bb4099ca53c70eadcd1d9d4be46b65c1e0b540528e619fd236ae09a"

    rpoint = "02f8460e855b091cec11ccf4a85064d4a8a7d3a2970b957a2165564b537d510bb4"  
    apoint = "029bc17aed9a0a5821b5b0425d8260d66f0529eb357a0b036765d68904152f618a"

    res = lit.rpc.DifferentResultsFraud(Sfirst=s1string, Hfirst=h1string, Ssecond=s2string, Hsecond=h2string, Rpoint=rpoint, Apoint=apoint)

    print(res)