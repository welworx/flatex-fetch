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

	// reDocRow matches each document row's opening <tr> in the unescaped
	// HTML from a "replacePortions" command's deltasToApply — confirmed
	// shape from live capture 2026-07-17: id="TID<formInstance>_<rowIdx>-0";
	// "Unread" appears in the class list only for unread documents (there is
	// also a "Gelesen am"/read-date column, C5, but the class is simpler).
	reDocRow = regexp.MustCompile(`<tr class="([^"]*)" id="TID\d+_(\d+)-\d+"`)

	// reDocDate/reDocName match the Datum/Beschreibung cells (C2/C4) within
	// a single row's HTML slice. The Dokumententyp cell (C3) is deliberately
	// not parsed — flatex-next has no per-document-type equivalent, so
	// Document no longer carries a category field for either UI.
	reDocDate = regexp.MustCompile(`class="C2[^"]*"[^>]*>([^<]*)</td>`)
	reDocName = regexp.MustCompile(`class="C4[^"]*"[^>]*><div class="Ellipsis">([^<]*)</div>`)
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

// --- flatex-next (the newer React-shell UI) ---
//
// Everything below mirrors flatex-next, reverse-engineered from a live
// Chrome netlog capture (2026-07-26, raw bytes included) of a real account
// that has switched to it. flatex-next is a different frontend on the same
// backend session/widget framework — the low-level primitives (tokenId
// rotation, windowId registration via engineStartUp, the fullPageReplace
// resync signal, the {"commands":[...]} AJAX envelope) are all identical
// and reused unchanged. What differs is the path prefix, the post-login
// sequence, and the archive widget's field names/markup.
//
// Login itself (GET pathLoginPage, POST pathSSO with the same
// userId/password/deviceDetails fields) is byte-for-byte identical —
// confirmed from the capture's own request bytes. The account's UI variant
// is only observable from where the POST /login.at/sso redirect chain
// finally lands, which Login detects and branches on.
const (
	// nextDesktopSegment is flatex-next's banking-app path segment — a fixed
	// literal like "login.at", NOT domain-parameterized like "banking-<domain>"
	// (confirmed: the capture's account uses the flatex.at domain and still
	// gets literal "next-desktop.at"). Unverified for flatex.de.
	nextDesktopSegment = "next-desktop.at"

	// nextArchiveAction is flatex-next's single consolidated action for the
	// dashboard/archive dialog — confirmed live, replacing the old UI's
	// separate documentArchiveListFormAction.do/accountOverviewFormAction.do/
	// headerAreaFormAction.do actions.
	nextArchiveAction = "overviewFormAction.do"

	// loginCommand and loginProgressAction are new steps flatex-next inserts
	// between the credentials POST and the old UI's direct
	// accountOverviewFormAction.do landing: /login.at/sso 302s to
	// loginCommand?loginData=<opaque token>, which itself 302s to
	// loginProgressAction. Both hops are followed automatically by Go's
	// default http.Client redirect handling — no code needed for them
	// beyond detecting the final landing path.
	loginProgressAction = "loginProgressFormAction.do"

	// cmdResumeLogin finalizes the session server-side. Confirmed required
	// (not just decorative UI bookkeeping): the capture's own
	// processCommandQueue response explicitly instructs the client to run
	// it — {"command":"executeServerCommand","serverCommand":"resumeLogin"}
	// — before any overviewFormAction.do request succeeds.
	cmdResumeLogin = "resumeLogin"
)

// Archive widget fields for flatex-next's document-archive dialog —
// confirmed from live capture, all under the fixed
// fullScreenSecondLevelWidgetList[0] dialog instance (numeric position, not
// a stable ID — same fragility class as the old UI's menu-position and
// combobox-index constants).
const (
	fieldNextOpenArchive = "headerAreaWidget.settingsButtonWidget.btnOpenDocumentArchive.clicked"
	fieldNextOverviewIdx = "selectedViewWidget.overviewEntryBlockSelectionWidget.selecteditemindex"

	fieldNextReadStateIdx    = "fullScreenSecondLevelWidgetList[0].secondLevelContentWidget.documentReadStatusSelectionWidget.selecteditemindex"
	fieldNextDateRangeIdx    = "fullScreenSecondLevelWidgetList[0].secondLevelContentWidget.dateRangeSelectionWidget.cbxDateRange.selecteditemindex"
	fieldNextDateRangeCustom = "fullScreenSecondLevelWidgetList[0].secondLevelContentWidget.dateRangeSelectionWidget.children[7].link.clicked"
	fieldNextScrollPos       = "fullScreenSecondLevelWidgetList[0].scrollposition"
	fieldNextReload          = "fullScreenSecondLevelWidgetList[0].secondLevelContentWidget.btnReload.clicked"
	fieldNextOpenDocFmt      = "fullScreenSecondLevelWidgetList[0].secondLevelContentWidget.children[%d].btnOpenDocument.clicked"

	// The date-range picker is a nested sub-dialog (thirdLevelWidgetList[0])
	// submitted as its own form — confirmed live: its POST carries only
	// these three fields, none of the parent dialog's state.
	fieldNextDateStart = "thirdLevelWidgetList[0].dtStartDate.text"
	fieldNextDateEnd   = "thirdLevelWidgetList[0].dtEndDate.text"
	fieldNextDateApply = "thirdLevelWidgetList[0].btnApply.clicked"

	idxNextOverviewDefault  = "0"
	idxNextReadStateAll     = "0"
	idxNextDateRangeDefault = "0"
	idxNextDateRangeCustom  = "6" // paired with fieldNextDateRangeCustom + explicit dates, like the old UI's idxRetrievalPeriodCustom
)

var (
	// reNextEntry matches one document row's opening div in the unescaped
	// HTML from a "replacePortions" command — confirmed shape from live
	// capture: class carries "Unread" only for unread documents (same
	// pattern as the old UI's row class), data-widgetname's children[N]
	// index is what fieldNextOpenDocFmt clicks to open/download it.
	reNextEntry = regexp.MustCompile(`class="(DocumentArchiveListEntryWidget[^"]*)" data-widgetname="fullScreenSecondLevelWidgetList\[0\]\.secondLevelContentWidget\.children\[(\d+)\]\.btnOpenDocument"`)

	// reNextDescription matches the row's own name/description text,
	// searched for within a bounded slice starting at reNextEntry's match.
	reNextDescription = regexp.MustCompile(`<div class="Description">(?:<!--[^>]*-->)?([^<]*)`)

	// reNextDateHeader matches a date-group header — flatex-next groups
	// archive entries under one header per date rather than the old UI's
	// per-row date column. Every reNextEntry match belongs to the nearest
	// preceding reNextDateHeader match in document order.
	reNextDateHeader = regexp.MustCompile(`<div class="DocumentDate CategoryLabel">\s*(?:<!--[^>]*-->)?\s*(\d{2}\.\d{2}\.\d{4})`)

	// reNextAnyMarker finds date headers and entries together, in document
	// order, so a single pass can track "current date" while walking rows.
	reNextAnyMarker = regexp.MustCompile(reNextDateHeader.String() + `|` + reNextEntry.String())

	// reNextDownloadURL extracts DocumentViewer.display's first argument —
	// the download URL — from a btnOpenDocument response's "execute"
	// command script (see downloadLocationNext). Confirmed shape from live
	// capture: DocumentViewer.display("/next-desktop.at/downloadData/...", "application/pdf").
	reNextDownloadURL = regexp.MustCompile(`DocumentViewer\.display\("([^"]+)"`)
)
