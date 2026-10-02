const KEY = "operatorToken";

function stored(): string {
  try { return localStorage.getItem(KEY) ?? ""; } catch { return ""; }
}

/** How this browser is signed in: through the auth proxy (nothing to type), or with the operator token
 *  kept in this browser only. */
class Session {
  state = $state<"checking" | "proxy" | "token" | "out">("checking");
  user = $state("");
  token = $state(stored());

  /** Ask the server who we are without a token: behind the auth proxy that is enough. */
  async check() {
    try {
      const res = await fetch("api/v1/whoami", { cache: "no-store" });
      if (res.ok) {
        const who = (await res.json()) as { user: string; via: string };
        if (who.via === "proxy") {
          this.user = who.user;
          this.state = "proxy";
          return;
        }
      }
    } catch { /* offline: fall through to the token */ }
    this.state = this.token ? "token" : "out";
  }

  signIn(token: string) {
    this.token = token.trim();
    try { localStorage.setItem(KEY, this.token); } catch { /* private mode: lasts for this tab */ }
    this.state = "token";
  }

  signOut() {
    this.token = "";
    try { localStorage.removeItem(KEY); } catch { /* nothing stored */ }
    this.state = "out";
  }

  /** The server refused us: a stale token, or the proxy session ended. */
  refused() {
    if (this.state === "proxy") void this.check();
    else this.signOut();
  }
}

export const session = new Session();
