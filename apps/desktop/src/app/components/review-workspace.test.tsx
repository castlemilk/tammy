import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useRef, useState } from "react";
import { expect, it } from "vitest";
import { WorkSearch } from "./review-workspace";

it("clears a local query with one named action and restores the caller's input focus", async () => {
  function Example() {
    const [query, setQuery] = useState("invoice");
    const ref = useRef<HTMLInputElement>(null);
    return (
      <WorkSearch
        inputRef={ref}
        label="Search invoices"
        clearLabel="Clear invoice search"
        value={query}
        onChange={setQuery}
      />
    );
  }
  render(<Example />);
  await userEvent.click(screen.getByRole("button", { name: "Clear invoice search" }));
  const input = screen.getByRole("searchbox", { name: "Search invoices" }) as HTMLInputElement;
  expect(input.value).toBe("");
  expect(document.activeElement).toBe(input);
  expect(screen.queryByRole("button", { name: "Clear invoice search" })).toBeNull();
});
