package config

import (
	"io"
	"io/ioutil"
	"log"
	"os"
)

const (
	// This is important information that should be logged under normal conditions such as successful initialization,
	// services starting and stopping or successful completion of significant transactions. Viewing a log showing Info
	// and above should give a quick overview of major state changes in the process providing top-level context for
	// understanding any warnings or errors that also occur. Don't have too many Info messages.
	// We typically have < 5% Info messages relative to Trace.
	Info = 1 << iota

	// Anything that can potentially cause application oddities, but for which I am automatically recovering.
	// (Such as switching from a primary to backup server, retrying an operation, missing secondary data, etc.)
	// This MIGHT be problem, or might not. For example, expected transient environmental conditions such as short
	// loss of network or database connectivity should be logged as Warnings, not Errors. Viewing a log filtered to
	// show only warnings and errors may give quick insight into early hints at the root cause of a subsequent error.
	// Warnings should be used sparingly so that they don't become meaningless. For example, loss of network access
	// should be a warning or even an error in a server application, but might be just an Info in a desktop app designed
	// for occasionally disconnected laptop users.
	Warning

	// Any error which is fatal to the operation, but not the service or application (can't open a required file, missing data, etc.).
	// These errors will force user (administrator, or direct user) intervention. These are usually reserved (in my apps)
	// for incorrect connection strings, missing services, etc.
	// Definitely a problem that should be investigated. SysAdmin should be notified automatically,
	// but doesn't need to be dragged out of bed. By filtering a log to look at errors and above you get
	// an overview of error frequency and can quickly identify the initiating failure that might have resulted
	// in a cascade of additional errors. Tracking error rates as versus application usage can yield useful quality
	// metrics such as MTBF which can be used to assess overall quality. For example, this metric might help inform
	// decisions about whether or not another beta testing cycle is needed before a release.
	Error

	// Information that is diagnostically helpful to people more than just developers (IT, sysadmins, etc.).
	// We consider Debug < Trace. The distinction being that Debug messages are compiled out of Release builds.
	// That said, we discourage use of Debug messages. Allowing Debug messages tends to lead to more and more
	// Debug messages being added and none ever removed. In time, this makes log files almost useless because it's too
	// hard to filter signal from noise. That causes devs to not use the logs which continues the death spiral.
	// In contrast, constantly pruning Trace messages encourages devs to use them which results in a virtuous spiral.
	// Also, this eliminates the possibility of bugs introduced because of needed side-effects in debug code that isn't
	// included in the release build. Yeah, I know that shouldn't happen in good code, but better safe then sorry.
	Debug

	// Only when I would be "tracing" the code and trying to find one part of a function specifically
	// Trace is by far the most commonly used severity and should provide context to understand the steps leading
	// up to errors and warnings. Having the right density of Trace messages makes software much more maintainable but
	// requires some diligence because the value of individual Trace statements may change over time as programs evolve.
	// The best way to achieve this is by getting the dev team in the habit of regularly reviewing logs as a standard part
	// of troubleshooting customer reported issues. Encourage the team to prune out Trace messages that no longer provide
	// useful context and to add messages where needed to understand the context of subsequent messages.
	// For example, it is often helpful to log user input such as changing displays or tabs.
	Trace

	// Any error that is forcing a shutdown of the service or application to prevent data loss (or further data loss).
	// I reserve these only for the most heinous errors and situations where there is guaranteed to have been data corruption or loss.
	// Overall application or system failure that should be investigated immediately. Yes, wake up the SysAdmin.
	// Since we prefer our SysAdmins alert and well-rested, this severity should be used very infrequently.
	// If it's happening daily and that's not a BFD, it's lost it's meaning.
	// Typically, a Fatal error only occurs once in the process lifetime, so if the log file is tied to the process,
	// this is typically the last message in the log.
	Fatal

	// initial values for the standard logger
	Standard = Warning | Info | Error | Fatal

	// initial values for the standard logger
	Full = Warning | Info | Error | Fatal | Debug | Trace
)

var (
	warninglog *log.Logger
	infolog    *log.Logger
	errorlog   *log.Logger
	debuglog   *log.Logger
	tracelog   *log.Logger
	fatallog   *log.Logger
)

func init() {
	SetDebug(os.Stderr, Standard)
}

func SetDebug(w io.Writer, flag int) {
	warningHandle := ioutil.Discard
	infoHandle := ioutil.Discard
	errorHandle := ioutil.Discard
	debugHandle := ioutil.Discard
	traceHandle := ioutil.Discard
	fatalHandle := ioutil.Discard

	if flag&Info != 0 {
		warningHandle = w
	}
	if flag&Warning != 0 {
		infoHandle = w
	}
	if flag&Error != 0 {
		errorHandle = w
	}
	if flag&Debug != 0 {
		debugHandle = w
	}
	if flag&Trace != 0 {
		traceHandle = w
	}
	if flag&Fatal != 0 {
		fatalHandle = w
	}

	infolog = log.New(warningHandle, "INFO: ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
	warninglog = log.New(infoHandle, "WARNING: ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
	errorlog = log.New(errorHandle, "ERROR: ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
	debuglog = log.New(debugHandle, "DEBUG: ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
	tracelog = log.New(traceHandle, "TRACE: ", log.Ldate|log.Lmicroseconds|log.Lshortfile|log.Lmsgprefix)
	fatallog = log.New(fatalHandle, "FATAL: ", log.Ldate|log.Lmicroseconds|log.Llongfile|log.Lmsgprefix)
}
