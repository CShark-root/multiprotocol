package service

import "testing"

// A pair printed by `xray mldsa65` (Xray 26.10.31): our derivation must
// produce the same verification key, or clients would reject the server.
const (
	xraySeed   = "VBqebZ6KuNikJSU8GOaK_D_Oyazb0KViph5sb1LzMhc"
	xrayVerify = "csVAPxDuWR94HQscaUHZx1MmIYmK6axQme2AB3X4usPjdXSiLPwks_CLm_W7B_TyeWVVAsUep0bQ4aAPca2R15VQ0DJtFmUHuSVp9IkByQGFi0W7s2h3Ti7hOASvQdwRVh6a6gyOnmuyoLDHp5o63naTAueKqbCRdrYEMesr-M97Pq2AJmk58PqhLrKBC7bbbX8AvvDfSW21MhbXavnhuPYQW76AFhkkfZ7Bsnfd2TPdiYGRKmXbvxf1UCiIzr0vPDdaoUf-CYtzTzVF_sl6-hXuM6QKGROSw7UKcKgwLsa4zKVKvFZhVeRx2PoniKLCAJNndTlWlukfONEa85HYQsQyxh-SxbXzIwhGJX24Si5TfbFHc6vR_ziPkexuUjJvaoQQdlRO0YemwYFd-7KVxOmxoqw7idErk9UPGUyma0l5dlukTE0sHG94Q1KFr6HPkAqi5Tozjvx89IZBs0YyPMeWEmX5_HCsSFFlMenOEs_UIrzzlar40hnxfOSHmZFx55BK2N_bWnN7ytlF-OvAL9wJAzl_da1-gEkUIpzNDtJq67CNDesPzwOS-Y1ieSU1NIylw1s5XLMGDn5yHWtgcQVP2UPxzDlaGGjzHc45jqFXFNnAgfphIxO2PUY945K-SehnAmG9bNmDgaAB7m9X-WmBHTdZkFdk6zzvsXo3AMPiws4RqfEx2z7BlHO2n99_6XrZ0TI8sfAEA_rD7VzH4fYQE5SraXz_aLtsI56veURNMKWzo7mp0HX7VtLVsveJUAkaETwmaRn7BWUNdtGZ-WohRIM_JlTIKChVIoaYWeYRWP87waRakJ4oORti_pmMR3Wei0ti_zaB4uqr3Ic4nJ2U7M8V0XdsK69RVLz1hs-abwu1TlOyRm1g874NJHebhln5JC4sccimW60iA8Wrc0WRlHnhsOEXmY7gLtP5ppTqwzLGBzsjKhYW1xvFjsMj2SHO0kplp1ZMjMc6P_680qtvpNKF6KlQMwXE0MkveBnA3lPMyShfWpivFP7W_h3RFYJYAwEwjvqqq9vlXXjMjWmxHvr4HXqKJYRjKwMKZfqcwUeaenmlEyKxcy-PlI_qBeLYe8agKQXYbsjmGd5U5uAjCvNYXEhtd1Tr79smf7oMDAO-4wW_PNElzJIyysSLmvdNFpHqwDrDbxlg4dRHue4AAKIHMl8W7VNxbgOMlKt4ncg8Ginw09H2AJ9YCNaObnNvGqO64vY3hPd2jDoPwp1Q67RdZvBILEPlgcOpbNYJM0l7wr9Mt9yW4Pj67LKDJF1A_yQFB5LtbTdPbzsGZDbA89uC_R6ZxUjnk37O5R6lH0LPpZUnfjoJcdOiuHbo3o-qTikPgcfbBP7jggB6SDsxzxGdAVZU3oSU7W2u44mB71RHdMLySpNG_AI-Z896LQkbLI869801LPMuMmHkLSNgrtQ-ss7qMhZwtJmx_FguCV_iLJrhScdTrJXRipHmv8M6oZ1-ic6EfEcKdLAmYewHkwEsGmGgQf847mtoREGovZaoC0Z0G49sVL6_1TAJ5ht0j0504Fb2uAiwTZqSVDhhhaKxxXR3SjPo6lu4DiEbquex9g9TXxIbeIGwBUkVDeZ4oMjjL75MGSkjwXuhMbw_HYh8kbDy2bUDXzP9l425iveoKwoFOHyrTHCd5FojtEQvf0TiB8ekWcs2iZrFJRHutEKoykg-Jtl3Qk_S3bvTrZ9aZ8ecqX9VBgfo_Up8ZAjgbPP0247KAqW1TPBm55xBdUY0ETuzE6FUcg8-kKVTW1h4sHAb8xQ5JcCxHrdtvbiaUtGbu4F12STihByxxWwOf1H_1QmAl1ZpSrEW1ZJwqpkEhu-jqPhVrj2bYwWgEAYjDCQWQv5s9XakmqF3ezqqrnn0ZufDRgmUOtRIlpbi0qayacUvvvhjGROStWPZzb0ot3ZmE3np8L49CU6-jHklvuUu0Ty4bjQGthkx63lNkodG6JU1ZY0cGjwBB0tjI6_17ziG9PkdLH9pyIyPQdi1UOdPHdcIlLFsI3jFrIDqdJNZrywCRlfamH_b0UNpaeGRhSfcZFj7YlSzrmQFS05Ho6S1ZVDczafQgP7oQLGFRmkflXOEyG-LN9kFCaRurX2V4BaBAl8TOtFIephy4ncRFVdOHfEScn_a60klTZABT39Z017P-JsTbjsS8SlzAjUAXzYCqqKC7risYuKu6Vj0-RxJ5vvUhjbEmeE0eYBr8P4DtQ0JFvnQzKkm-lVJnduqofA9y6Nv4C2MHIhCxn4n-nAUSAA54ETmHi9W_mMU-QimokK3hu9xJbIlQuNA4I8EGuOoDdCn_89r3QKp3RDtTtc8CHPrPF0G0JWu3vR3hnE_MC4mIEUp_c1aeHs9BGCTq0FFk_xOpUaJcYb4IINKx8Iw3_MoIqsnFxOZRF45MyHtfzOGz8qzaM7qOkxCcSKKfynYkW2kjNYsr79YPImQ-2vZlThp9RYuZeYSkmggfl59NKwFH-8ytK2Yb0CtXPpox86SCCTEwd2uvASEU0fuGuEjUYZk70lw6YWIo9gqhb57OvX2_A2Jl2fLjVtiExt-GHbnDfOVUmIWxlCvKExLmr6AMVy6wrhHZynMLiM"
)

func TestMLDSA65MatchesXray(t *testing.T) {
	_, verify, err := MLDSA65FromSeed(xraySeed)
	if err != nil {
		t.Fatal(err)
	}
	if verify != xrayVerify {
		t.Fatalf("verification key differs from Xray's:\n got %.40s…\nwant %.40s…", verify, xrayVerify)
	}
	seed, verify, err := NewMLDSA65()
	if err != nil || len(seed) != 43 || len(verify) != 2603 {
		t.Fatalf("new pair: %d/%d %v", len(seed), len(verify), err)
	}
}

func TestValidRangeAndVersion(t *testing.T) {
	for v, want := range map[string]bool{"100": true, "100-1000": true, "1000-100": false, "-5": false, "1-2-3": false, "a": false} {
		if validRange(v) != want {
			t.Errorf("validRange(%q) = %v", v, !want)
		}
	}
	for v, want := range map[string]bool{"25.9.11": true, "25": true, "256.1": false, "1.2.3.4": false} {
		if validClientVer(v) != want {
			t.Errorf("validClientVer(%q) = %v", v, !want)
		}
	}
}
