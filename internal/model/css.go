package model

const (
	// Login screen

	BUTTON_PLAY_NOW     string = "button.play-now"                  // "Play free now" button
	BUTTON_LOGIN        string = `button[onclick="login('show');"]` // open "log in" form
	TEXT_FIELD_LOGIN    string = "input#lEmail"                     // login input field
	TEXT_FIELD_PASSWORD string = "input#lPass"                      // password input field
	BUTTON_AUTH         string = "button#btnLogin"                  // "Log in" button
	OVERLAY_LOADING     string = "div.preloader.exo.xl-text"        // loading overlay

	// Main screen

	BUTTON_MAIN_HUBS        string = `button[onclick="popup('hubs.php','Hubs');"]`                                      // "Hubs" button
	BUTTON_MAIN_ACCOUNT     string = `li.text-center[onclick="popup('banking.php','Banking');"]`                        // "Banking" button
	BUTTON_MAIN_COMPANY     string = "div#smallMainMenu div#mapAcList"                                                  // "Company, staff & highscore" button
	BUTTON_MAIN_FLEET       string = "div#smallMainMenu div#mapRoutes"                                                  // "Fleet & routes" button
	BUTTON_MAIN_FUEL        string = `div#smallMainMenu div#mapMaint[data-original-title="Fuel & co2"]`                 // "Fuel & co2" button
	BUTTON_MAIN_MAINTENANCE string = `div#smallMainMenu div#mapMaint[data-original-title="Maintenance"]`                // "Maintenance" button
	BUTTON_MAIN_FINANCE     string = `div#smallMainMenu div#mapMaint[data-original-title="Finance, Marketing & Stock"]` // "Finance, Marketing & Stock" button
	BUTTON_MAIN_BONUS       string = `div#smallMainMenu div#mapMaint[data-original-title="Bonus & Increase"]`           // "Bonus & Increase" button
	ICON_FREE_REWARDS       string = "div#smallMainMenu img#bonusDutyFreeIconAlert"                                     // "Free rewards" icon on the "Bonus" button

	// "Banking" pop-up

	LIST_ACCOUNT_ACCOUNTS        string = "div#bankingAction > table > tbody > tr" // List of accounts web elements
	TEXT_ACCOUNT_ACCOUNT_NAME    string = "tr > td:nth-child(1)"                   // Account name in the child element
	TEXT_ACCOUNT_ACCOUNT_BALANCE string = "tr > td:nth-child(2)"                   // Account balance in the child element

	// Buttons for switching tabs in pop-ups

	BUTTON_COMMON_TAB1        string = "#popBtn1"                    // switch to tab 1
	BUTTON_COMMON_TAB2        string = "#popBtn2"                    // switch to tab 2
	BUTTON_COMMON_TAB3        string = "#popBtn3"                    // switch to tab 3
	BUTTON_COMMON_CLOSE_POPUP string = `span[onclick="closePop();"]` // close pop-up window

	// "Flight info" elements (left side of main screen)

	ICON_FI_LOUNGE_ALERT  string = "div#flightInfo span#loungeAlertIcon"                                                           // lounge "alert" icon (means that lounge needs repair)
	BUTTON_ALLIANCE_INFO  string = `div#flightInfo span[onclick="popup('alliance.php','Alliance');"]`                              // "Alliance" info button
	BUTTON_FI_OVERVIEW    string = `div#flightInfo div#flightInfoSecContainer button[onclick="popup('overview.php','Overview');"]` // "Overview" button
	BUTTON_FI_DEPART_ALL  string = "div#flightInfo button.btn-xs:nth-child(2)"                                                     // "Depart All" button
	TEXT_FI_DEPART_AMOUNT string = "div#flightInfo span#listDepartAmount"                                                          // text showing number of aircraft ready for departure

	// "Overview" pop-up

	TEXT_OVERVIEW_AIRLINE_REPUTATION              string = "div#popup div#popContent div.col-6:nth-child(4)"                                                                                  // PAX airline reputation text
	TEXT_OVERVIEW_CARGO_REPUTATION                string = "div#popup div#popContent div.col-6:nth-child(5)"                                                                                  // Cargo airline reputation text
	TEXT_OVERVIEW_FLEET_SIZE                      string = "div#popup div#popContent div.col-sm-6:nth-child(7) > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(3) > td:nth-child(2)" // Fleet size text
	TEXT_OVERVIEW_AC_PENDING_DELIVERY             string = "div#popup div#popContent div.col-sm-6:nth-child(7) > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(4) > td:nth-child(2)" // Aircraft pending delivery text
	TEXT_OVERVIEW_ROUTES                          string = "div#popup div#popContent div.col-sm-6:nth-child(7) > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(5) > td:nth-child(2)" // Routes counter text
	TEXT_OVERVIEW_HUBS                            string = "div#popup div#popContent div.col-sm-6:nth-child(7) > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(6) > td:nth-child(2)" // Hubs counter text
	TEXT_OVERVIEW_AC_PENDING_MAINTENANCE          string = "div#popup div#popContent div.col-sm-6:nth-child(7) > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(7) > td:nth-child(2)" // Aircraft pending maintenance text
	TEXT_OVERVIEW_HANGAR_CAPACITY                 string = "div#popup div#popContent div.col-sm-6:nth-child(7) > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(9) > td:nth-child(2)" // Hangar capacity text
	TEXT_OVERVIEW_AC_INFLIGHT                     string = "div#popup div#popContent div.col-sm-6:nth-child(8) > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(2) > td:nth-child(2)" // Aircraft in-flight text
	TEXT_OVERVIEW_SHARE_PRICE                     string = "div#popup div#popContent div.col-sm-6:nth-child(8) > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(3) > td:nth-child(2)" // Share price text
	TEXT_OVERVIEW_FLIGHTS_OPERATED                string = "div#popup div#popContent div.col-sm-6:nth-child(8) > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(4) > td:nth-child(2)" // Flights operated text
	TEXT_OVERVIEW_PASSENGERS_ECONOMY_TRANSPORTED  string = "div#popup div#popContent div.col-sm-6:nth-child(8) > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(5) > td:nth-child(2)" // Economy passengers transported text
	TEXT_OVERVIEW_PASSENGERS_BUSINESS_TRANSPORTED string = "div#popup div#popContent div.col-sm-6:nth-child(8) > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(6) > td:nth-child(2)" // Business passengers transported text
	TEXT_OVERVIEW_PASSENGERS_FIRST_TRANSPORTED    string = "div#popup div#popContent div.col-sm-6:nth-child(8) > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(7) > td:nth-child(2)" // First class passengers transported text
	TEXT_OVERVIEW_CARGO_TRANSPORTED_LARGE         string = "div#popup div#popContent div.col-sm-6:nth-child(8) > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(8) > td:nth-child(2)" // Large cargo transported text
	TEXT_OVERVIEW_CARGO_TRANSPORTED_HEAVY         string = "div#popup div#popContent div.col-sm-6:nth-child(8) > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(9) > td:nth-child(2)" // Heavy cargo transported text

	// "Alliance" pop-up

	LIST_ALLIANCE_MEMBERS                    string = "div#popup div#popContent div#member-container div#member-container-box > table > tbody > tr" // List of alliance members web elements
	TEXT_ALLIANCE_MEMBER_ID                  string = "id"                                                                                          // member ID attribute
	TEXT_ALLIANCE_MEMBER_NAME                string = "tr > td:nth-child(1) > a"                                                                    // member name text
	TEXT_ALLIANCE_MEMBER_SHARE_PRICE         string = "tr > td:nth-child(2)"                                                                        // member share price text
	TEXT_ALLIANCE_MEMBER_CONTRIBUTED_TOTAL   string = "tr > td:nth-child(3)"                                                                        // member contributed total text
	TEXT_ALLIANCE_MEMBER_CONTRIBUTED_PER_DAY string = "tr > td:nth-child(4)"                                                                        // member contributed per day text
	TEXT_ALLIANCE_MEMBER_FLIGHTS             string = "tr > td:nth-child(6)"                                                                        // member flights text
	TEXT_ALLIANCE_MEMBER_SEASON_MONEY        string = "tr > td:nth-child(8)"                                                                        // member season contributed money text
	TEXT_ALLIANCE_CONTRIBUTED_TOTAL          string = "div#popup div#popContent div#member-container tr.td-sort.bg-light > td:nth-child(3)"         // Total contributed money text
	TEXT_ALLIANCE_CONTRIBUTED_PER_DAY        string = "div#popup div#popContent div#member-container tr.td-sort.bg-light > td:nth-child(4)"         // Money contributed per day text
	TEXT_ALLIANCE_FLIGHTS                    string = "div#popup div#popContent div#member-container tr.td-sort.bg-light > td:nth-child(6)"         // Flights text
	TEXT_ALLIANCE_SEASON_MONEY               string = "div#popup div#popContent div#member-container tr.td-sort.bg-light > td:nth-child(8)"         // Season contributed money text

	// "Hubs" pop-up

	BUTTON_HUBS_LOUNGES_MAINTENANCE       string = "div#popContent button#loungeBtn"                                                                       // "Lounges & Maintenance" -> "Maintenance" tab button
	LIST_HUBS_LOUNGES                     string = "div#popContent table.table.table-sm.m-text > tbody > tr"                                               // List of lounges web elements
	TEXT_HUBS_LOUNGES_LOUNGE_NAME         string = "tr > td:nth-child(1)"                                                                                  // Lounge name text
	TEXT_HUBS_LOUNGES_LOUNGE_WEAR_PERCENT string = "tr > td:nth-child(2) > b:nth-child(1)"                                                                 // Lounge wear percentage text
	TEXT_HUBS_LOUNGES_LOUNGE_REPAIR_COST  string = "tr > td:nth-child(2) > span:nth-child(3)"                                                              // Lounge repair cost text
	BUTTON_HUBS_LOUNGES_LOUNGE_REPAIR     string = "tr > td:nth-child(3) > button:nth-child(1)"                                                            // Lounge repair button
	BUTTON_HUBS_LOUNGES_BACK_TO_HUBS      string = `div#popContent button[onclick="popup('hubs.php','Hubs');"]`                                            // "<- Back" button to Hubs main tab
	LIST_HUBS_HUBS                        string = "div#hubList > div.row.mt-1.opa.rounded"                                                                // List of hubs web elements
	ELEMENT_HUB                           string = "div.row.mt-1.opa.rounded > div:nth-child(3) > div:nth-child(1)"                                        // each hub element for FromNode usage
	TEXT_HUBS_HUB_NAME                    string = "div.p-2.col-9.exo.m-text > b"                                                                          // Hub name text
	TEXT_HUBS_HUB_DEPARTURES              string = "div.row.mt-1.opa.rounded > div:nth-child(3) > div:nth-child(1) > div:nth-child(1) > span:nth-child(3)" // Hub departures text
	TEXT_HUBS_HUB_ARRIVALS                string = "div.row.mt-1.opa.rounded > div:nth-child(3) > div:nth-child(1) > div:nth-child(2) > span:nth-child(3)" // Hub arrivals text
	TEXT_HUBS_HUB_PAX_DEPARTED            string = "div.row.mt-1.opa.rounded > div:nth-child(4) > div:nth-child(1) > div:nth-child(1) > span:nth-child(3)" // Hub departed PAX text
	TEXT_HUBS_HUB_PAX_ARRIVED             string = "div.row.mt-1.opa.rounded > div:nth-child(4) > div:nth-child(1) > div:nth-child(2) > span:nth-child(3)" // Hub arrived PAX text
	BUTTON_HUBS_HUB_MANAGE                string = "div#hubDetail button.btn.btn-danger.btn-xs-real"                                                       // "Manage hub" button
	TEXT_HUBS_HUB_MANAGE_REPAIR_COST      string = "#loungeRepairCost"                                                                                     // Lounge repair cost text in "Manage hub" tab
	BUTTON_HUBS_HUB_MANAGE_REPAIR         string = "div#hubDetail.hidden button#loungeRepairBtn"                                                           // "Repair lounge" button in "Manage hub" tab
	BUTTON_HUBS_HUB_MANAGE_BACK           string = "#hubReturnBtn > button:nth-child(1)"                                                                   // "<- Back" button to Hubs main tab from "Manage hub" tab
	ICON_HUBS_CATERING                    string = "div.row.mt-1.opa.rounded span.glyphicons-fast-food"                                                    // Catering icon in hub element
	BUTTON_HUBS_ADD_CATERING              string = "div#hubDetail button.btn-success:nth-child(1)"                                                         // "Add catering" button in "Manage hub" tab
	ELEM_HUBS_CATERING_OPTION_3           string = "div#caterMain div.col-4:nth-child(4)"                                                                  // Catering option 3 element
	SELECT_HUBS_CATERING_DURATION         string = "div#caterMain select#durationSelector"                                                                 // Catering duration select element
	SELECT_HUBS_CATERING_AMOUNT           string = "div#caterMain select#caterAmount"                                                                      // Catering amount select element
	TEXT_HUBS_CATERING_COST               string = "div#caterMain span#sumCost"                                                                            // Catering cost text
	BUTTON_HUBS_CATERING_BUY              string = "div#caterMain button#btnCaterDo"                                                                       // "Buy catering" button

	// "Company" pop-up

	TEXT_COMPANY_RANK                           string = "div.text-secondary"                                                                                                 // Company rank text
	TEXT_COMPANY_STAFF_TRAINING_POINTS          string = "span#tPoints"                                                                                                       // Staff training points text
	TEXT_COMPANY_STAFF_PILOT_SALARY             string = "#pilotSalary"                                                                                                       // Pilot salary text
	TEXT_COMPANY_STAFF_PILOT_MORALE             string = "#pilotMorale"                                                                                                       // Pilot morale text
	BUTTON_COMPANY_STAFF_PILOT_SALARY_UP        string = "#pilot_main > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(3) > td:nth-child(1) > button:nth-child(1)"    // Pilot salary increase button
	BUTTON_COMPANY_STAFF_PILOT_SALARY_DOWN      string = "#pilot_main > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(3) > td:nth-child(2) > button:nth-child(1)"    // Pilot salary decrease button
	TEXT_COMPANY_STAFF_CREW_SALARY              string = "#crewSalary"                                                                                                        // Crew salary text
	TEXT_COMPANY_STAFF_CREW_MORALE              string = "#crewMorale"                                                                                                        // Crew morale text
	BUTTON_COMPANY_STAFF_CREW_SALARY_UP         string = "#crew_main > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(3) > td:nth-child(1) > button:nth-child(1)"     // Crew salary increase button
	BUTTON_COMPANY_STAFF_CREW_SALARY_DOWN       string = "#crew_main > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(3) > td:nth-child(2) > button:nth-child(1)"     // Crew salary decrease button
	TEXT_COMPANY_STAFF_ENGINEER_SALARY          string = "#engineerSalary"                                                                                                    // Engineer salary text
	TEXT_COMPANY_STAFF_ENGINEER_MORALE          string = "#engineerMorale"                                                                                                    // Engineer morale text
	BUTTON_COMPANY_STAFF_ENGINEER_SALARY_UP     string = "#engineer_main > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(3) > td:nth-child(1) > button:nth-child(1)" // Engineer salary increase button
	BUTTON_COMPANY_STAFF_ENGINEER_SALARY_DOWN   string = "#engineer_main > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(3) > td:nth-child(2) > button:nth-child(1)" // Engineer salary decrease button
	TEXT_COMPANY_STAFF_TECHNICIAN_SALARY        string = "#techSalary"                                                                                                        // Technician salary text
	TEXT_COMPANY_STAFF_TECHNICIAN_MORALE        string = "#techMorale"                                                                                                        // Technician morale text
	BUTTON_COMPANY_STAFF_TECHNICIAN_SALARY_UP   string = "#tech_main > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(3) > td:nth-child(1) > button:nth-child(1)"     // Technician salary increase button
	BUTTON_COMPANY_STAFF_TECHNICIAN_SALARY_DOWN string = "#tech_main > table:nth-child(1) > tbody:nth-child(1) > tr:nth-child(3) > td:nth-child(2) > button:nth-child(1)"     // Technician salary decrease button

	// "Fuel" pop-up

	TEXT_FUEL_FUEL_PRICE    string = "div#fuelMain span#sumCost"                  // fuel price text
	TEXT_FUEL_FUEL_HOLDING  string = "div#fuelMain #holding"                      // fuel holding text
	TEXT_FUEL_FUEL_CAPACITY string = "div#fuelMain span.s-text:nth-child(4)"      // fuel capacity text
	TEXT_FIELD_FUEL_AMOUNT  string = "input#amountInput"                          // fuel amount input field
	BUTTON_FUEL_BUY         string = "div#fuelMain button.btn-block:nth-child(2)" // "Buy fuel" button

	// "Maintenance" pop-up

	BUTTON_MAINTENANCE_BASE_ONLY        string = "div#maintAction button#baseOnly"                                                                                                           // "Base only" maintenance filter button
	BUTTON_MAINTENANCE_SORT_BY_CHECK    string = `div#maintAction button[onclick="sortMaint('check');"]`                                                                                     // "Sort by A-Check hours" button
	BUTTON_MAINTENANCE_SORT_BY_WEAR     string = `div#maintAction button[onclick="sortMaint();"]`                                                                                            // "Sort by wear %" button
	LIST_MAINTENANCE_AC_LIST            string = "div#maintAction div#acListView > div.at-base"                                                                                              // List of aircraft web elements
	TEXT_MAINTENANCE_AC_A_CHECK_HOURS   string = "data-hours"                                                                                                                                // aircraft A-Check hours text
	TEXT_MAINTENANCE_AC_WEAR_PERCENT    string = "data-wear"                                                                                                                                 // aircraft wear percentage text
	TEXT_MAINTENANCE_AC_REG_NUMBER      string = "data-reg"                                                                                                                                  // aircraft registration number text
	TEXT_MAINTENANCE_AC_TYPE            string = "data-type"                                                                                                                                 // aircraft type text
	BUTTON_MAINTENANCE_A_CHECK          string = `div[role="group"] button:nth-child(1)`                                                                                                     // "A-Check" button
	BUTTON_MAINTENANCE_REPAIR           string = `div[role="group"] button:nth-child(2)`                                                                                                     // "Repair" button
	BUTTON_MAINTENANCE_MODIFY           string = `div[role="group"] button:nth-child(3)`                                                                                                     // "Modify" button
	CHECKBOX_MAINTENANCE_MODIFY_MOD1    string = "div#typeModify table.table.table-sm.exo > tbody:nth-child(1) > tr:nth-child(1) > td:nth-child(1) > label:nth-child(1) > span:nth-child(2)" // modification 1 checkbox
	CHECKBOX_MAINTENANCE_MODIFY_MOD2    string = "div#typeModify table.table.table-sm.exo > tbody:nth-child(1) > tr:nth-child(2) > td:nth-child(1) > label:nth-child(1) > span:nth-child(2)" // modification 2 checkbox
	CHECKBOX_MAINTENANCE_MODIFY_MOD3    string = "div#typeModify table.table.table-sm.exo > tbody:nth-child(1) > tr:nth-child(3) > td:nth-child(1) > label:nth-child(1) > span:nth-child(2)" // modification 3 checkbox
	TEXT_MAINTENANCE_A_CHECK_TOTAL_COST string = "div#typeCheck div.col-6:nth-child(6) > div:nth-child(3)"                                                                                   // A-Check total cost text
	TEXT_MAINTENANCE_REPAIR_TOTAL_COST  string = "div#typeRepair div:nth-child(4) > div:nth-child(3)"                                                                                        // Repair total cost text
	TEXT_MAINTENANCE_MODIFY_TOTAL_COST  string = "div#typeModify div.row > div.col-6.text-center > span.text-danger.font-weight-bold"                                                        // Modify total cost text
	BUTTON_MAINTENANCE_PLAN_CHECK       string = "div#typeCheck button.btn.btn-xs-real.btn-danger"                                                                                           // "A-Check" plan button
	BUTTON_MAINTENANCE_PLAN_REPAIR      string = "div#typeRepair button.btn.btn-xs-real.btn-danger"                                                                                          // "Repair" plan button
	BUTTON_MAINTENANCE_PLAN_MODIFY      string = "div#typeModify button.btn-danger:nth-child(1)"                                                                                             // "Modify" plan button

	// "Finance" pop-up

	BUTTON_FINANCE_MARKETING_NEW_COMPANY               string = "div#financeAction button#newCampaign"                                              // "+ New campaign" button
	ELEM_FINANCE_MARKETING_LIST                        string = "div#financeAction #active-campaigns > table:nth-child(1) > tbody:nth-child(1)"     // List of marketing campaigns web elements
	ELEM_FINANCE_MARKETING_INC_AIRLINE_REP             string = "div#financeAction table.table:nth-child(2) > tbody:nth-child(1) > tr:nth-child(1)" // "Increase airline reputation" campaign element
	ELEM_FINANCE_MARKETING_INC_CARGO_REP               string = "div#financeAction table.table:nth-child(2) > tbody:nth-child(1) > tr:nth-child(2)" // "Increase cargo reputation" campaign element
	ELEM_FINANCE_MARKETING_ECO_FRIENDLY                string = "div#financeAction table.table:nth-child(2) > tbody:nth-child(1) > tr:nth-child(3)" // "Eco-friendly" campaign element
	SELECT_FINANCE_MARKETING_COMPANY_DURATION          string = "div#financeAction select#dSelector"                                                // marketing campaign duration select element
	OPTION_FINANCE_MARKETING_INC_AIRLINE_REP_24H_VALUE string = "6"                                                                                 // element option value for 24 hours duration for "Increase airline reputation" campaign
	OPTION_FINANCE_MARKETING_INC_CARGO_REP_24H_VALUE   string = "6"                                                                                 // element option value for 24 hours duration for "Increase cargo reputation" campaign
	TEXT_FINANCE_MARKETING_INC_AIRLINE_REP_COST        string = "div#financeAction span#c4"                                                         // cost text for "Increase airline reputation" campaign
	TEXT_FINANCE_MARKETING_INC_CARGO_REP_COST          string = "div#financeAction span#c4"                                                         // cost text for "Increase cargo reputation" campaign
	TEXT_FINANCE_MARKETING_ECO_FRIENDLY_COST           string = "div#financeAction button.btn-danger:nth-child(1)"                                  // cost text for "Eco-friendly" campaign
	BUTTON_FINANCE_MARKETING_INC_AIRLINE_REP_BUY       string = "div#financeAction button#c4Btn"                                                    // "Buy" button for "Increase airline reputation" campaign
	BUTTON_FINANCE_MARKETING_INC_CARGO_REP_BUY         string = "div#financeAction button#c4Btn"                                                    // "Buy" button for "Increase cargo reputation" campaign
	BUTTON_FINANCE_MARKETING_ECO_FRIENDLY_BUY          string = TEXT_FINANCE_MARKETING_ECO_FRIENDLY_COST                                            // "Buy" button for "Eco-friendly" campaign
	TEXT_FINANCE_MARKETING_INC_AIRLINE_REP_DURATION    string = "div#financeAction table > tbody > tr:nth-child(1) > td.hasCountdown > span"        // duration text for active "Increase airline reputation" campaign
	TEXT_FINANCE_MARKETING_INC_CARGO_REP_DURATION      string = "div#financeAction table > tbody > tr:nth-child(2) > td.hasCountdown > span"        // duration text for active "Increase cargo reputation" campaign
	TEXT_FINANCE_MARKETING_ECO_FRIENDLY_DURATION       string = "div#financeAction table > tbody > tr:nth-child(3) > td.hasCountdown > span"        // duration text for active "Eco-friendly" campaign

	// "Bonus" pop-up

	BUTTON_BONUS_DUTY_FREE_TAB string = "div#popContent button#dutyFree"                // "Duty free" tab button
	BUTTON_BONUS_CLAIM_GIFT    string = "div#popContent div#dutyFree button#claim_gift" // "Claim gift" button
)
