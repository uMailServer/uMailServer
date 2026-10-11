import { describe, it, expect } from "vitest"
import { replySubject, forwardSubject, quoteForReply, forwardBody } from "./quote"

describe("F6933 reply/forward helpers", () => {
  it("does not stack prefixes", () => {
    expect(replySubject("Re: hi")).toBe("Re: hi")
    expect(replySubject("RE: hi")).toBe("Re: hi")
    expect(replySubject("hi")).toBe("Re: hi")
    expect(forwardSubject("Fwd: hi")).toBe("Fwd: hi")
    expect(forwardSubject("FW: hi")).toBe("Fwd: hi")
    expect(forwardSubject("hi")).toBe("Fwd: hi")
  })
  it("quotes lines and strips html", () => {
    expect(quoteForReply("a@b", "d", "x\r\ny")).toContain("> x\n> y")
    expect(quoteForReply("a@b", "d", "<p>hi</p><script>evil()</script>")).not.toContain("evil")
    expect(quoteForReply("a@b", "d", "> old")).toContain(">> old")
  })
  it("builds forward body", () => {
    expect(forwardBody("a", "d", "s", ["x@y"], "body")).toContain("Forwarded message")
  })
})
