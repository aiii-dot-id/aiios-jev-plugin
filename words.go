package main

const (
	askSummary        = "Ask a calibrated judge (Jev) typed questions about state, files, or each of many items. Answers are probabilities to weigh, never a decision, a permission, or who your human is. Sent to the judge your operator set; each reply names it."
	stateWords        = "What every question needs: the evidence, the goal, the rule. Send only what the questions need. A file you name goes to the judge unread by you: name only what may go there. Text you did not write can steer the answer about it."
	questionsWords    = "Each one judgment a person could make at a glance, literal and complete: Jev answers exactly what is written. A question without a type is a yes/no question. Pair a choice or a box with a noul asking whether anything fits. Or name a file of questions."
	instructionsWords = "Name what to judge: the part of state in backticks (`ticket.text`), or `item`. Phrase it so a true answer is yes; one condition, no 'or'. No counting, arithmetic or date comparison: do those yourself."
	criteriaWords     = "choice: option names that say what they mean, each with a description. score: levels lowest first, each a situation on its own. noul (optional): true and false, each a concrete situation, not 'not X'."
	itemsWords        = "Things to ask the same questions about, each placed into each question; one row per item comes back, so you need not read them. An item can steer its own answer: read one before you act on it."
	rankWords         = "With items: order rows by this question, highest first: a noul's yes, a choice's top option or `id.option`, a score's level. Without it, rows keep item order."
	longContextWords  = "Use Jev's long context (262k tokens; slower and less accurate). Set it only when a reply said the request was too long for the default limits."
	fromWords         = "With items: a reply's next, passed back whole. It continues the same screen with the same model, and is refused if the state, the questions or the items changed."

	modelsSummary = "List the models of the judge your operator set, each with its release date and description, and name that judge"
)

const (
	flagLongContext = "slower and less accurate (TypeSafe)"
	flagImages      = "TypeSafe: not yet production-ready"
	flagOrientation = "pixels stored rotated (EXIF %d); boxes refer to the stored pixels"
)

const (
	unconfiguredJev      = "Set the Jev API key on the plugin card, or set backend to local with local_url"
	unconfiguredLocal    = "backend is local and local_url is empty; set it on the plugin card to the address of the judge on the operator's network"
	configurationInvalid = "the %s setting %s; correct it on the plugin card"
)

const (
	notSent        = "Not sent: nothing reached the judge (%s), and nothing was answered or charged"
	outcomeUnknown = "Sent to the judge (%s), and no answer came back: the answers are unknown and may have been charged"
	againOne       = "; asking again is safe"
	againOneMaybe  = "; asking again asks again, and may be charged again"
	againItems     = "; nothing in this call was consumed: the same from asks these items again"
	outOfTime      = "Not sent: out of time. Preparing this call left %d ms of the invocation's %d s, less than one attempt's %d ms beside the reply; nothing reached the judge (%s)"
	noHostTime     = "Not sent: the host gave this invocation no start time, so the time left for an attempt is not known; nothing reached the judge (%s)"
	statusRefused  = "The judge (%s) answered HTTP %d; no answers accepted, and nothing asked again"
	retryAfter     = "; retry_after_s: %d, the seconds it asks before another request"
	noRetryAfter   = "; it named no time to wait"
	judgeReason    = "; the judge said: %s"
	judgeReasonCut = "… (cut at the %d-byte reply budget; the rest is not kept)"
	tooLongOne     = "; the judge's limits were exceeded: asking again with long_context true sends it slower and less accurately"
	tooLongItems   = "; the judge's limits were exceeded: the same from with long_context true judges this batch onward, slower and less accurately"
	tooLongAlready = "; the judge's limits were exceeded even with long_context"
	replyTooLarge  = "The judge's reply exceeds the %d bytes the host carries"
)

const (
	replyWouldNotFit  = "The answers to these questions could reach %d bytes, over the %d-byte reply budget; %d of the %d questions would fit. Nothing was sent"
	rowWouldNotFit    = "One item's row of answers to these questions could reach %d bytes, and the reply leaves %d of its %d-byte budget for rows; %d of the %d questions would fit. Nothing was sent"
	sharedTooLarge    = "The state and questions take %d bytes and leave no room for an item in one request of %d. Nothing was sent"
	sharedTooManyRef  = "The state and questions name %d files and leave no reference for an item; one call resolves at most %d. Nothing was sent"
	requestTooLarge   = "The request would be %d bytes; one request holds %d. Pass less state or fewer questions. Nothing was sent"
	referencesTooMany = "%s: more than %d files named in one call; name fewer. Nothing was sent"
	fileTooLarge      = "%s: %d bytes; one request has %d left for it. Nothing was sent"
	fileNotText       = "%s: neither UTF-8 text nor a PNG or JPEG image"
	fileUnreadable    = "%s: the host could not read it (%s: %s)"
	imageOutOfBounds  = "%s: %d × %d pixels; the judge takes 280 to 1568 on each side. Resize it first"
	imageUnreadable   = "%s: a %s image whose size its header does not give"
	listingIncomplete = "%s: the folder holds more than the %d names the host lists, and this screen needs a name beyond them. Narrow the folder, or name each file as a $file item"
	screenChanged     = "from belongs to another screen: the state, the questions or the items changed since that reply. Start again without from to judge the new screen. Nothing was sent"
	questionsFileBad  = "%s: not a JSON list of questions"
)

const (
	itemTooLarge  = "%d bytes; one request holds %d, and the state and questions leave %d for an item"
	itemTooLargeQ = "%d bytes, placed in each of %d questions, make a request of %d bytes; one request holds %d"
	itemTooMany   = "names %d files; the state and questions leave %d of the %d one call resolves"
	droppedNote   = "members the judge added, left out of this reply: %s"
)
