/**
 * A string that exists only inside the fake client's returned data (src/api/testing.tsx).
 *
 * It is USED there, not merely declared: an unused constant is tree-shaken out of a bundle even when
 * its module is wrongly imported, and a sentinel that cannot reach the bundle cannot prove anything.
 * G5 fails if it appears in the built frontend.
 */
export const FAKE_CLIENT_SENTINEL = "LITE_TEST_FAKE_CLIENT_7f3a9c";
