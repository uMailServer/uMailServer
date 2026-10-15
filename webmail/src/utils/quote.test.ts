import { describe, it, expect } from "vitest"
import { replySubject, forwardSubject, quoteForReply, forwardBody, replyAllRecipients } from "./quote"

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

describe("reply-all address extraction", () => {
  it("takes the real address when the display name contains its own <address>", () => {
    const r = replyAllRecipients('"Eve <victim@x.test>" <attacker@evil.test>', ["Bob <bob@x.test>"], [], "me@x.test")
    expect(r.to).toEqual(["attacker@evil.test", "bob@x.test"])
    expect(JSON.stringify(r)).not.toContain("victim@x.test")
  })
  it("still handles plain and bare forms and removes the caller", () => {
    const r = replyAllRecipients("Ayşe Yılmaz <ayse@x.test>", ["me@x.test", "carol@x.test"], ["dave@x.test"], "Me <me@x.test>")
    expect(r.to).toEqual(["ayse@x.test", "carol@x.test"])
    expect(r.cc).toEqual(["dave@x.test"])
  })
})
