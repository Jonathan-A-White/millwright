package application

import (
	"context"
	"fmt"
	"strings"
)

// posternAudioMimes are the voice notes section 14 names.
var posternAudioMimes = map[string]bool{
	"audio/webm": true, "audio/ogg": true, "audio/mp4": true, "audio/mpeg": true,
}

// isPosternAudio reports whether mime is a voice note's, its parameters
// (audio/webm;codecs=opus) aside.
func isPosternAudio(mime string) bool {
	kind, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(mime)), ";")
	return posternAudioMimes[strings.TrimSpace(kind)]
}

// isVoiceNote reports whether m is a voice note this host hears: a message
// carrying audio, with a transcriber to hear it. With none configured, a
// voice note is read like any other message with an attachment.
func (i PosternInbox) isVoiceNote(m PosternInboxMessage) bool {
	return i.Transcriber != nil && m.Attachment != nil && isPosternAudio(m.Attachment.Mime)
}

// applyVoice hears a voice note the Governor sent, section 14: the audio,
// already downloaded and decrypted to path, is transcribed; in a bead's
// thread the transcript is written on the bead as his words
// ("GOVERNOR (voice) via postern, txid …: <transcript>"); it is sent back to
// him in the same thread as a transcript of the note (re its txid, role
// transcript), so he sees exactly what was heard; and it is mailed to the
// Mayor. A note that cannot be downloaded or heard is refused, and the
// Mayor is told why, with where the audio is.
func (i PosternInbox) applyVoice(ctx context.Context, m PosternInboxMessage, outcome, path string) (posternApplied, error) {
	result := posternApplied{Kind: "voice", Bead: m.Thread, Txid: m.Txid}
	if path == "" {
		why := outcome
		if why == "" {
			why = "the voice note was not downloaded"
		}
		result.Refused, result.Detail = true, why
		return result, i.mail(ctx, "Voice note not heard: "+m.Thread,
			fmt.Sprintf("The Governor's voice note (txid %s, thread %s) could not be read: %s.", m.Txid, m.Thread, why))
	}
	heard, err := i.Transcriber.Transcribe(ctx, path)
	if err != nil {
		result.Refused, result.Detail = true, "transcribing it failed: "+err.Error()
		return result, i.mail(ctx, "Voice note not heard: "+m.Thread,
			fmt.Sprintf("The Governor's voice note (txid %s, thread %s) could not be transcribed: %s.\n\nThe audio is at %s.", m.Txid, m.Thread, err, path))
	}
	transcript := strings.TrimSpace(heard)
	if transcript == "" {
		transcript = "(nothing was heard)"
	}
	caption := strings.TrimSpace(m.Text)

	var problems []string
	if m.ThreadIsBead {
		comment := fmt.Sprintf("GOVERNOR (voice) via postern, txid %s: %s", m.Txid, transcript)
		if caption != "" {
			comment += fmt.Sprintf(" [caption: %s]", caption)
		}
		if err := i.Tracker.CommentOnStory(ctx, m.Thread, comment); err != nil {
			problems = append(problems, fmt.Sprintf("it was not written on %s: %v", m.Thread, err))
		}
		result.Detail = fmt.Sprintf("[audio: %s]", path)
	} else {
		result.Detail = transcript
	}
	if i.Sender != nil {
		back := PosternSendRequest{Class: "message", Text: transcript, Re: m.Txid, Role: PosternRoleTranscript}
		switch {
		case m.ThreadIsBead:
			back.Thread = m.Thread
		case m.Thread != PosternGeneralThread:
			back.Topic = m.Thread
		}
		if _, err := i.Sender.Run(ctx, back); err != nil {
			problems = append(problems, fmt.Sprintf("it was not sent back to the Governor: %v", err))
		}
	}

	body := fmt.Sprintf("The Governor said, in a voice note by postern (txid %s, thread %s):\n\n%s", m.Txid, m.Thread, transcript)
	if caption != "" {
		body += fmt.Sprintf("\n\nwith the words: %s", caption)
	}
	body += fmt.Sprintf("\n\nThe audio is at %s.", path)
	for _, problem := range problems {
		body += "\n\nBut " + problem + "."
	}
	return result, i.mail(ctx, fmt.Sprintf("Voice: %s: %s", m.Thread, clippedTo(transcript, 60)), body)
}
