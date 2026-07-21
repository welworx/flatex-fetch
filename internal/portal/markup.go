package portal

import "regexp"

// Everything in this file mirrors the real flatex.at web portal protocol,
// reverse-engineered from live captures (HAR/netlog, plus the login form's
// own HTML) of the actual portal in 2026-07. The original design's
// assumptions — based on now-outdated third-party tools — turned out to be
// wrong on nearly every endpoint (wrong host, wrong login mechanism, wrong
// listing/download shape). Login, listing, and download are all confirmed
// working against a real account as of 2026-07-16 (see the design spec's
// "Open risk" section for the outcome record).

const (
	// portalHost is the real host for both login and the banking app — NOT
	// www.<domain> as originally assumed. Confirmed via live capture.
	portalHost = "konto.flatex.at"

	pathLoginPage = "/login.at/loginIFrameFormAction.do" // GET, seeds tokenId before login
	pathSSO       = "/login.at/sso"                      // POST, real credential submission (from the login form's own HTML)

	// accountOverviewAction loads right after a successful login (seen in
	// every live capture); used to confirm authentication succeeded and to
	// re-seed tokenId for the banking-app context.
	accountOverviewAction = "accountOverviewFormAction.do"

	// Banking-app paths embed the profile's domain (/banking-flatex.at/...).
	// flatex.at is the default and only verified target; flatex.de must
	// work without code changes but stays untested.
	archiveListAction = "documentArchiveListFormAction.do" // confirmed
	headerAreaAction  = "headerAreaFormAction.do"          // confirmed: the top nav menu's own form action
	ajaxCommandAction = "ajaxCommandServlet"               // confirmed: generic AJAX-engine command dispatcher
)

// engineStartUp fields. Every fresh full-page load runs this command
// before anything else — it's how the portal's JS registers the client's
// (client-generated) windowId with the server. Skipping it is why every
// archive request kept coming back as an unrecognized-session
// "fullPageReplace", even immediately after a resync GET: the resync GET
// itself is a full page load and would normally trigger this too.
const (
	fieldCommand                = "command"
	fieldWindowIDPreviouslyUsed = "windowIdPreviouslyUsed"
	fieldDeviceData             = "deviceData"
	cmdEngineStartUp            = "engineStartUp"
)

// The document-archive menu entry's position, confirmed from live capture:
// menu.items[4].items[0].clicked=true. Numeric position, not a stable ID —
// if flatex reorders the top nav menu this breaks silently (same fragility
// as the filter combobox indices, see design spec "Fragility note").
const (
	fieldMenuDocumentArchiveClicked = "menu.items[4].items[0].clicked"
	fieldSearchEditField            = "searchEditFieldWidget.editField.text"
)

// Login form fields, captured verbatim from the real login form's HTML
// (id="loginIFrameForm_Form", action="sso", method="post").
const (
	fieldUserID        = "userId"
	fieldPassword      = "password"
	fieldDeviceDetails = "deviceDetails" // JSON blob, see deviceDetails struct
	fieldWindowWidth   = "windowWidth"
	fieldWindowHeight  = "windowHeight"
)

var (
	// tokenId is embedded in page responses as a JS function call
	// (webcore.setTokenId("...")) and rotates; echoed as X-tokenId on
	// subsequent AJAX requests. windowId is client-generated (see
	// newWindowID), not server-issued — the original design assumed both
	// came from a simple ":"/"=" JS assignment, which doesn't match the
	// real markup.
	reTokenID = regexp.MustCompile(`setTokenId\(\s*["']([A-Za-z0-9_-]+)["']`)
)

// --- archive listing/download (Plan 3) ---

// Filter combobox indices — all confirmed against the real combobox HTML
// (item_0 aria-label="Alle"/"Alle Dokumente"/"Alle Dokumenttypen" for
// account/category/readState respectively; item_6 aria-label="Individueller
// Zeitraum" for the period selector).
const (
	idxAccountDefault        = "0" // "Alle Dokumente" (all accounts)
	idxCategoryAll           = "0" // "Alle Dokumenttypen"
	idxReadStateAll          = "0" // "Alle"
	idxRetrievalPeriodCustom = "6" // "Individueller Zeitraum", paired with explicit dates
)

// Archive filter/download form fields, captured from live traffic.
const (
	fieldDateFrom        = "dateRangeComponent.startDate.text"
	fieldDateTo          = "dateRangeComponent.endDate.text"
	fieldAccount         = "accountSelection.account.selecteditemindex"
	fieldCategory        = "documentCategory.selecteditemindex"
	fieldReadState       = "readState.selecteditemindex"
	fieldRetrievalPeriod = "dateRangeComponent.retrievalPeriodSelection.selecteditemindex"
	fieldStoreSettings   = "storeSettings.checked"
	fieldSelectAllRows   = "documentArchiveListTable.headerWidgets[0].checked"
	fieldApplyFilter     = "applyFilterButton.clicked"
	fieldDownloadClicked = "btnDocumentDownload.clicked"
	rowSelectionPrefix   = "documentArchiveListTable.rowSelectionSupport[" // + "N].checked"

	// fieldRetrieveMore is the archive's "load more" button field, per a
	// live HAR capture of the browser's own scroll-triggered load-more:
	// that request sends *only* this field. NOT currently used in
	// production code — two live attempts to page past the archive's
	// first results page using it have both failed:
	//   1. merged into the full archiveFilterForm (2026-07-20): returned
	//      0 rows and disrupted the session for later requests.
	//   2. sent bare, per the HAR capture, but following this package's
	//      explicit custom-date-range filter submission (2026-07-21):
	//      also returned 0 rows. The HAR's own scroll session never
	//      applied a custom filter before scrolling, so it only proves
	//      the bare shape works for the *default* (unfiltered) view —
	//      not that it composes with an explicit filter submission,
	//      which fetch/list always do.
	// See TestE2EPagination in e2e_test.go before trying again; a HAR
	// capture of an explicit custom-range filter followed by scrolling
	// would settle whether/how these compose.
	fieldRetrieveMore = "btnRetrieveMore.clicked"
)

var (
	// Row markers in a filter response — one per document row, confirmed
	// from live capture. Only the index is used; per-row document content
	// (names, dates) lives in the same response but is deliberately not
	// parsed here.
	reRowSelection = regexp.MustCompile(`rowSelectionSupport\[(\d+)\]`)

	// reDocRow matches each document row's opening <tr> in the unescaped
	// HTML from a "replacePortions" command's deltasToApply — confirmed
	// shape from live capture 2026-07-17: id="TID<formInstance>_<rowIdx>-0";
	// "Unread" appears in the class list only for unread documents (there is
	// also a "Gelesen am"/read-date column, C5, but the class is simpler).
	reDocRow = regexp.MustCompile(`<tr class="([^"]*)" id="TID\d+_(\d+)-\d+"`)

	// reDocDate/reDocCategory/reDocName match the Datum/Dokumententyp/
	// Beschreibung cells (C2/C3/C4) within a single row's HTML slice.
	reDocDate     = regexp.MustCompile(`class="C2[^"]*"[^>]*>([^<]*)</td>`)
	reDocCategory = regexp.MustCompile(`class="C3[^"]*"[^>]*><div class="Ellipsis">([^<]*)</div>`)
	reDocName     = regexp.MustCompile(`class="C4[^"]*"[^>]*><div class="Ellipsis">([^<]*)</div>`)
)

// capWarning is the literal UI text the portal shows when a listing's
// results were capped at 100 documents (confirmed live, 2026-07-21):
// "Es werden nur die ersten 100 Dokumente dargestellt." ("Only the first
// 100 documents are shown."). Detected as a plain substring of the raw
// (still JSON-encoded) response body — the message contains no characters
// JSON string-escaping would alter, so it survives unescaped either way.
//
// tableMarker is present in any response that actually rendered the
// archive results widget — confirmed from a HAR capture (2026-07-21): a
// custom date-range filter too wide for the portal to answer comes back
// with neither this marker nor capWarning, just near-empty content (the
// date-picker widgets alone re-rendering) — a distinct failure mode from
// capping, with no explicit signal beyond this marker's absence.
const (
	capWarning  = "Es werden nur die ersten 100 Dokumente dargestellt."
	tableMarker = "documentArchiveListTable"
)
