/** A piece of a chat message: plain text, or the text of a Twitch emote (`emote` is its id). */
export interface ChatFragment {
  text: string;
  emote?: string;
}

/** The message a chat message answers: only live chat knows it, a VOD replay does not. */
export interface ChatReply {
  /** Id of the answered message, to find it again among the messages already received. */
  id: string;
  user: string;
  /** What the answered message said, cut short. */
  text: string;
}

export interface ChatMessage {
  id: string;
  /** Seconds into the VOD; absent for a live message. */
  at?: number;
  user: string;
  /** `#RRGGBB`. */
  color?: string;
  fragments: ChatFragment[];
  reply?: ChatReply;
}

export interface ChatReplayPage {
  messages: ChatMessage[];
  /** Opaque cursor of the page that follows; absent after the last one. */
  next?: string;
}
