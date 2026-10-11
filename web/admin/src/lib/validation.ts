// Client-side mirrors of the server rules in internal/api/validators.go so
// the operator sees the reason before a round trip. The server stays the
// authority; these only pre-check.

const SPECIAL = "!@#$%^&*()_+-=[]{}|;':\",./<>?`~\\";

export function passwordError(pw: string): string | null {
  if (pw.length < 8) return "Password must be at least 8 characters";
  if (pw.length > 128) return "Password exceeds maximum length of 128 characters";
  if (!/[A-Z]/.test(pw)) return "Password must contain at least one uppercase letter";
  if (!/[a-z]/.test(pw)) return "Password must contain at least one lowercase letter";
  if (!/[0-9]/.test(pw)) return "Password must contain at least one digit";
  if (![...pw].some((c) => SPECIAL.includes(c))) return "Password must contain at least one special character";
  return null;
}

export function domainError(name: string): string | null {
  if (!name) return "Domain name is required";
  if (name.includes("..") || /[/\\]/.test(name)) return "Domain name contains invalid characters";
  if (name.length > 253) return "Domain name is too long";
  for (const label of name.replace(/\.$/, "").split(".")) {
    if (!label || label.length > 63 || label.startsWith("-") || label.endsWith("-")) {
      return "Domain name has an invalid label";
    }
    if (!/^[A-Za-z0-9_-]+$/.test(label)) return "Domain name contains invalid characters";
  }
  return null;
}

export function emailError(email: string): string | null {
  if (!email) return "Email is required";
  if (email.includes("..") || /[/\\\r\n\0]/.test(email)) return "Email contains invalid characters";
  if (email.split("@").length !== 2) return "Email must contain exactly one @ character";
  const [user, domain] = email.split("@");
  if (!user || !domain) return "Email format is invalid";
  if (user.length > 64) return "Email local part is too long";
  return domainError(domain);
}
