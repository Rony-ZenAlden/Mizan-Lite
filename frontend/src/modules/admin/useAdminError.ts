import { useTranslation } from "@/app/providers/PreferencesProvider";
import { isBindingError } from "@/lib/wails";

/**
 * Renders a failed call as text in the active language.
 *
 * One helper, used by every administration screen, so the rules the backend refuses with —
 * "the last administrator's role cannot be removed", "you cannot deactivate the account you are
 * signed in with" — reach the user as sentences rather than as codes.
 *
 * Those refusals are the reason this exists. They are not developer errors: they are the system
 * explaining a rule, and each one is the moment the user learns it.
 */
export function useErrorText(): (error: unknown) => string {
  const { t } = useTranslation();
  return (error: unknown) =>
    isBindingError(error) ? t(error.messageKey, error.params) : t("app.unknown_error");
}
