package fakereap

// Fixture catalogue: Singapore products modelled on what the Reap SG sandbox returned during the
// 2026-10-09 probe (backend/seed/catalog.yaml "probe_seen"). Merchant names are the exact
// products[].merchant.name strings Reap returns (backend/seed/vendors.yaml reap_merchant_name);
// Domain is what merchantPreference.merchantName must be. A few products come from merchants that
// are NOT on the allow-list so callers' filtering is exercised. Prices are SGD cents.

// Merchant names as Reap returns them.
const (
	MerchantPopular     = "Popular Bookstore"
	MerchantKinokuniya  = "Books Kinokuniya Singapore"
	MerchantCommonMan   = "Common Man Coffee Roasters SG"
	MerchantAlchemist   = "Alchemist"
	MerchantDutchColony = "Dutch Colony Coffee Co."
	MerchantBettr       = "Bettr Coffee"
	MerchantPPP         = "PPP Coffee"
	MerchantGryphon     = "Gryphon Singapore"
	MerchantPryce       = "Pryce Tea"
	MerchantCamelNuts   = "Camel Nuts"
	MerchantBoxgreen    = "Boxgreen"
	MerchantIrvins      = "IRVINS SG"
	MerchantPlain       = "Plain Vanilla Online"
	MerchantIntertech   = "Intertech Hardware Singapore"
	MerchantPupsik      = "Pupsik Singapore"
	MerchantShoppy      = "Shoppy"
	MerchantMetro       = "Metro Singapore Departmental Store - Celebrating 69 Years in SG"
	MerchantSpectrum    = "Spectrum Store"
	MerchantUgreen      = "UGREEN SG"
	MerchantAnker       = "---" // sic: the sandbox really returns "---" for anker.com.sg
	MerchantErgoTune    = "ErgoTune"
	MerchantPrism       = "PRISM+ Singapore"

	// Not on the allow-list.
	MerchantMustafa       = "ShopMustafa"
	MerchantStationeryPal = "Stationery Pal"
	MerchantWaangoo       = "Waangoo"
)

// FixtureVariant is one purchasable variant.
type FixtureVariant struct {
	ID               string
	Name             string
	Options          map[string]string // option name -> label, e.g. {"Colour": "Blue"}
	PriceCents       int64
	Available        bool
	RequiresShipping bool
}

// FixtureProduct is one product; Variants[0] is the default variant.
type FixtureProduct struct {
	ID          string
	Domain      string // merchant domain (merchantPreference.merchantName)
	Merchant    string // merchant.name as returned by Reap
	Name        string
	Description string
	OptionNames []string // ordered option groups; empty for single-variant products
	Variants    []FixtureVariant
}

func simple(id, domain, merchant, name string, cents int64) FixtureProduct {
	return FixtureProduct{
		ID: "prd_" + id, Domain: domain, Merchant: merchant, Name: name,
		Description: name + ". Ships within Singapore.",
		Variants:    []FixtureVariant{{ID: "var_" + id, Name: "Default", PriceCents: cents, Available: true, RequiresShipping: true}},
	}
}

// opt builds a single-option product: labels[i] costs cents[i]; unavailable lists labels out of stock.
func opt(id, domain, merchant, name, optName string, labels []string, cents []int64, unavailable ...string) FixtureProduct {
	p := FixtureProduct{
		ID: "prd_" + id, Domain: domain, Merchant: merchant, Name: name,
		Description: name + ". Ships within Singapore.", OptionNames: []string{optName},
	}
	out := map[string]bool{}
	for _, u := range unavailable {
		out[u] = true
	}
	for i, l := range labels {
		p.Variants = append(p.Variants, FixtureVariant{
			ID: "var_" + id + "_" + slug(l), Name: l, Options: map[string]string{optName: l},
			PriceCents: cents[i], Available: !out[l], RequiresShipping: true,
		})
	}
	return p
}

func unavailable(p FixtureProduct) FixtureProduct {
	for i := range p.Variants {
		p.Variants[i].Available = false
	}
	return p
}

// DefaultProducts returns a fresh copy of the fixture catalogue.
func DefaultProducts() []FixtureProduct {
	return []FixtureProduct{
		// ---------- Paper & Stationery: popular.com.sg ----------
		simple("pop_ik_copier_a4_80g", "popular.com.sg", MerchantPopular, "IK Signature Copier Paper 80g A4 500's", 710),
		simple("pop_paperone_a4_80g", "popular.com.sg", MerchantPopular, "PaperOne Copier Paper 80g A4 500's", 790),
		simple("pop_ik_copier_carton", "popular.com.sg", MerchantPopular, "IK Signature Copier Paper 80g A4 500's (1 Carton)", 3550),
		unavailable(simple("pop_double_a_a4_70g", "popular.com.sg", MerchantPopular, "Double A Copier Paper 70g A4 500's", 820)),
		opt("pop_pilot_rexgrip", "popular.com.sg", MerchantPopular, "PILOT Rexgrip Ballpoint Pen 0.7mm", "Colour",
			[]string{"Blue", "Black", "Red"}, []int64{155, 155, 155}, "Red"),
		simple("pop_postit_654_5ss", "popular.com.sg", MerchantPopular, "Post-it 654-5SS Super Sticky Notes 3x3 (5 pads)", 1200),
		simple("pop_stabilo_swing_4", "popular.com.sg", MerchantPopular, "STABILO Swing Cool Highlighter Set of 4", 800),
		simple("pop_max_hd10nx", "popular.com.sg", MerchantPopular, "MAX Stapler HD-10NX", 790),
		simple("pop_max_staples_10_1m", "popular.com.sg", MerchantPopular, "MAX Staples 10-1M", 65),
		opt("pop_pilot_vboard", "popular.com.sg", MerchantPopular, "Pilot V Board Master Whiteboard Marker", "Colour",
			[]string{"Black", "Blue", "Red", "Green"}, []int64{175, 175, 175, 175}),
		simple("pop_bazic_ring_file", "popular.com.sg", MerchantPopular, "POP Bazic A4 2D Ring File 25MM", 490),
		simple("pop_centre_data_envelope", "popular.com.sg", MerchantPopular, "Centre Document Bag Data Envelope A4", 110),
		simple("pop_pentel_ztt605", "popular.com.sg", MerchantPopular, "PENTEL Retractable Correction Tape ZTT605", 200),
		simple("pop_kokuyo_campus_a4", "popular.com.sg", MerchantPopular, "KOKUYO A4 Campus Notebook Grid 5mm 40 Sheets", 775),
		simple("pop_energizer_aa_4", "popular.com.sg", MerchantPopular, "Energizer Max Battery AA 4pcs E91BP4M", 990),
		simple("pop_energizer_aaa_4", "popular.com.sg", MerchantPopular, "Energizer Max Battery AAA 4pcs E92BP4M", 990),

		// ---------- kinokuniya.com.sg ----------
		simple("kino_kokuyo_campus_a4", "kinokuniya.com.sg", MerchantKinokuniya, "KOKUYO Campus Notebook A4 Dotted Ruled 30 Sheets", 690),
		simple("kino_pilot_g2_07", "kinokuniya.com.sg", MerchantKinokuniya, "PILOT G2 Gel Ballpoint Pen 0.7mm Blue", 260),
		simple("kino_a4_copier_80g", "kinokuniya.com.sg", MerchantKinokuniya, "Kinokuniya A4 Copier Paper 80gsm 500 sheets", 850),
		simple("kino_a4_envelope", "kinokuniya.com.sg", MerchantKinokuniya, "Kraft A4 Document Envelope (10 pcs)", 450),

		// ---------- Coffee & Tea ----------
		opt("cm_22_martin_250g", "commonmancoffeeroasters.com", MerchantCommonMan, "22 Martin Espresso Blend Coffee Beans 250g", "Grind",
			[]string{"Whole Bean", "Espresso", "Filter"}, []int64{2050, 2050, 2050}),
		simple("cm_daily_driver_caps", "commonmancoffeeroasters.com", MerchantCommonMan, "Daily Driver Coffee Capsules (Nespresso compatible, 10 pcs)", 1350),
		simple("cm_el_diviso_drip", "commonmancoffeeroasters.com", MerchantCommonMan, "Drip Bags El Diviso (6 pcs)", 2100),
		simple("alc_dark_matter_200g", "alchemist.com.sg", MerchantAlchemist, "Dark Matter Espresso Blend Coffee Beans 200g", 1800),
		simple("dc_espresso_250g", "dutchcolony.sg", MerchantDutchColony, "Dutch Colony Espresso Blend Coffee Beans 250g", 1900),
		simple("bettr_espresso_250g", "bettrcoffee.com", MerchantBettr, "Bettr Barista Espresso Blend Coffee Beans 250g", 2200),
		opt("ppp_supernova_drip", "pppcoffee.com", MerchantPPP, "Supernova Drip Bags", "Size",
			[]string{"Box of 10", "Box of 20"}, []int64{3000, 5500}),
		simple("gry_earl_grey_lavender", "gryphontea.com", MerchantGryphon, "Earl Grey Lavender Tea Tin", 1291),
		opt("gry_genmaicha", "gryphontea.com", MerchantGryphon, "Genmaicha Green Tea", "Size",
			[]string{"Tin 50g", "Tin 100g"}, []int64{916, 1526}),
		simple("pryce_earl_grey", "prycetea.com", MerchantPryce, "Pryce Earl Grey Tea Tin", 1400),

		// ---------- Pantry & Snacks ----------
		simple("camel_fancy_mixed_1kg", "camelnuts.com", MerchantCamelNuts, "Fancy Mixed Nuts 1kg", 2700),
		simple("bg_healthylicious_box", "boxgreen.co", MerchantBoxgreen, "Healthylicious Variety Box", 2800),
		simple("irvins_chips_95g", "irvinsaltedegg.com", MerchantIrvins, "IRVINS Salted Egg Potato Chips (95g)", 950),
		simple("pv_cookies_box6", "plainvanilla.com.sg", MerchantPlain, "Box of 6 Mixed Cookies", 2551),

		// ---------- Cleaning & Hygiene ----------
		simple("it_tsb_sanitizer", "intertech-hardware.com", MerchantIntertech, "TSB Hand Sanitizer Original 500ml", 330),
		simple("it_selleys_mpc_500", "intertech-hardware.com", MerchantIntertech, "Selleys Multi Purpose Cleaner 500ml", 515),
		simple("it_garbage_bag", "intertech-hardware.com", MerchantIntertech, "Intertech Garbage Bag Roll (Large)", 750),
		simple("pupsik_sanitizer", "pupsik.sg", MerchantPupsik, "Pupsik Antibacterial Hand Sanitizer 60ml", 450),
		opt("shoppy_bin_bags", "shoppy.sg", MerchantShoppy, "Trash Bin Garbage Bag Rolls", "Size",
			[]string{"Small", "Medium", "Large"}, []int64{590, 750, 890}),
		opt("shoppy_microfiber", "shoppy.sg", MerchantShoppy, "Microfiber Kitchen Cleaning Cloth", "Pack",
			[]string{"6 Pieces", "12 Pieces"}, []int64{990, 1890}),
		simple("metro_nudy_rudy_wash", "metro.com.sg", MerchantMetro, "Nudy Rudy Sea Salt Suds Hand Wash 500ml", 1186),
		simple("spectrum_hand_wash", "spectrumstore.sg", MerchantSpectrum, "Botanical Hand Wash 500ml", 1350),

		// ---------- Electronics & Office ----------
		opt("ug_70427_usbc_100w", "ugreen.com.sg", MerchantUgreen, "UGREEN 70427 100W USB C to USB C Cable", "Length",
			[]string{"1m", "2m", "3m"}, []int64{899, 1299, 1599}),
		opt("ug_25910_hdmi21", "ugreen.com.sg", MerchantUgreen, "UGREEN 25910 HDMI 2.1 Cable 8K/4K", "Length",
			[]string{"1m", "2m", "3m"}, []int64{1599, 1899, 2199}),
		simple("ug_45000_usbc_hub", "ugreen.com.sg", MerchantUgreen, "UGREEN 45000 USB C Hub 6-in-1", 3999),
		simple("anker_735_65w", "anker.com.sg", MerchantAnker, "Anker Charger 735 Powerport 65W USB C", 3890),
		simple("anker_usbc_cable_100w", "anker.com.sg", MerchantAnker, "Anker USB C to USB C Cable 100W 1.8m", 1299),
		opt("ergo_float_arm", "ergotune.com", MerchantErgoTune, "ErgoTune Float Monitor Arm", "Type",
			[]string{"Single", "Dual"}, []int64{7900, 13900}),
		simple("ergo_joobie_lite", "ergotune.com", MerchantErgoTune, "ErgoTune Joobie Lite Ergonomic Chair", 36900),
		opt("ergo_supreme", "ergotune.com", MerchantErgoTune, "ErgoTune Supreme Ergonomic Chair", "Edition",
			[]string{"Standard", "Pro"}, []int64{54900, 59900}),
		opt("prism_x240", "prismplus.sg", MerchantPrism, "PRISM+ X240 24\" Monitor 100Hz", "Model",
			[]string{"X240", "X240 Pro"}, []int64{14900, 17900}),

		// ---------- NOT allow-listed (callers must filter these out) ----------
		simple("mustafa_a4_copier", "mustafa.com.sg", MerchantMustafa, "A4 Copier Paper 80g 500 sheets", 650),
		simple("spal_ballpoint_07", "stationerypal.com", MerchantStationeryPal, "Ballpoint Pen 0.7mm Blue (box of 12)", 120),
		simple("waangoo_usbc_cable", "waangoo.com", MerchantWaangoo, "USB C to USB C Cable 100W 1m", 590),
	}
}
