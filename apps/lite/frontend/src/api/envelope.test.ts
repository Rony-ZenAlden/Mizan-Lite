import { describe, expect, it } from "vitest";
import {
  BindingError,
  CODE_BRIDGE_UNAVAILABLE,
  CODE_CALL_FAILED,
  CODE_MALFORMED_RESPONSE,
  CODE_UNKNOWN,
  unwrap,
} from "./envelope";

async function codeOf(promise: Promise<unknown>): Promise<string> {
  try {
    await promise;
  } catch (error) {
    expect(error).toBeInstanceOf(BindingError);
    return (error as BindingError).code;
  }
  throw new Error("expected the call to throw");
}

describe("unwrap", () => {
  it("returns the data of a successful envelope", async () => {
    await expect(unwrap(async () => ({ ok: true, data: { locale: "ar" } }))).resolves.toEqual({ locale: "ar" });
  });

  it("returns falsy data as it is, not as a failure", async () => {
    await expect(unwrap(async () => ({ ok: true, data: 0 }))).resolves.toBe(0);
    await expect(unwrap(async () => ({ ok: true, data: [] as string[] }))).resolves.toEqual([]);
  });

  it("throws the backend's code for a failed envelope", async () => {
    const failed = unwrap(async () => ({
      ok: false,
      data: null,
      error: { code: "lite.settings.invalid_locale", messageKey: "lite.settings.invalid_locale", params: { value: "fr" } },
    }));
    await expect(failed).rejects.toMatchObject({ apiError: { code: "lite.settings.invalid_locale", params: { value: "fr" } } });
  });

  it("gives a failure with no error a known code rather than undefined", async () => {
    expect(await codeOf(unwrap(async () => ({ ok: false, data: null })))).toBe(CODE_UNKNOWN);
  });

  it("treats a synchronous throw as a missing bridge", async () => {
    // What a generated binding does with no Wails runtime: it dereferences window.go and throws
    // before any promise exists.
    const missing = () => {
      throw new TypeError("Cannot read properties of undefined (reading 'api')");
    };
    expect(await codeOf(unwrap(missing))).toBe(CODE_BRIDGE_UNAVAILABLE);
  });

  it("treats a rejected call as a failed call, and does not leak its text", async () => {
    try {
      await unwrap(() => Promise.reject(new Error("runtime.go:42 panic: secret")));
      throw new Error("expected a rejection");
    } catch (error) {
      expect((error as BindingError).code).toBe(CODE_CALL_FAILED);
      expect((error as BindingError).message).not.toContain("secret");
    }
  });

  it.each([null, undefined, "text", 42, {}, { ok: "yes" }])("refuses a malformed response: %s", async (response) => {
    expect(await codeOf(unwrap(async () => response as never))).toBe(CODE_MALFORMED_RESPONSE);
  });
});
