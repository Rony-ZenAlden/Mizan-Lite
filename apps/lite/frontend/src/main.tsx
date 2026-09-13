import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { createClient } from "@/api/client";
import { Root } from "@/app/Root";
import "./index.css";

const container = document.getElementById("root");
if (container) {
  createRoot(container).render(
    <StrictMode>
      <Root client={createClient()} />
    </StrictMode>,
  );
}
