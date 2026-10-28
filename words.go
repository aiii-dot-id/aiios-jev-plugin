package main

const (
	querySummary = "State and questions are sent to the judge the operator set: TypeSafe's hosted Jev or an endpoint on the operator's network. Ask focused questions: noul for probability of yes, choice among options, score on described levels. Answers are evidence"

	modelsSummary = "List the model choices of the judge the operator set, for the model setting"

	unconfiguredJev = "Set the Jev API key on the plugin card, or set backend to local with local_url"

	unconfiguredLocal = "backend is local and local_url is empty; set it on the plugin card to the address of the judge on the operator's network"

	configurationInvalid = "the %s setting %s; correct it on the plugin card"

	notSent = "The request was not sent to the judge; nothing was answered or charged, and asking again is safe"

	outcomeUnknown = "The request was sent and no complete answer came back; the judge may have answered and charged, and asking again may charge again"

	statusRefused = "The judge answered HTTP %d; no answers accepted and no retry made"

	retryAfter = "; it asks for %d seconds before another request"

	noRetryAfter = "; it named no time to wait"

	replyTooLarge = "Reply exceeds the %d-byte reply cap; submit fewer questions or shorter option descriptions"
)
