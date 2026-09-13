// The ONLY file that imports the generated bindings (gate G1, enforced by ESLint).
//
// Every method Go binds must be referenced here (gate G2, a Go test), and every function here must be
// called from a screen or the shell (gate G3, a Vitest over the TypeScript AST). A method on either
// side with no partner fails a build, which is what "zero unconnected bindings" has to mean to hold.
//
// Types are not written by hand: each function's result type is inferred from the file Wails
// generated from the Go struct, so a field renamed in Go is a compile error here.
import * as App from "../../wailsjs/go/api/App";
import * as Settings from "../../wailsjs/go/api/Settings";
import { api } from "../../wailsjs/go/models";
import { unwrap, type Plain } from "./envelope";

export type BootStatus = Plain<api.BootStatusDTO>;
export type Health = Plain<api.HealthDTO>;
export type SettingsState = Plain<api.SettingsDTO>;
export type SettingsInput = Plain<api.SettingsInput>;

export function createClient() {
  return {
    app: {
      bootStatus: () => unwrap(App.BootStatus),
      health: () => unwrap(App.Health),
    },
    settings: {
      get: () => unwrap(Settings.Get),
      update: (input: SettingsInput) => unwrap(() => Settings.Update(api.SettingsInput.createFrom(input))),
    },
  };
}

export type Client = ReturnType<typeof createClient>;
