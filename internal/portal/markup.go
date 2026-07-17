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
)

var (
	// Row markers in a filter response — one per document row, confirmed
	// from live capture. Only the index is used; per-row document content
	// (names, dates) lives in the same response but is deliberately not
	// parsed here.
	reRowSelection = regexp.MustCompile(`rowSelectionSupport\[(\d+)\]`)
)
