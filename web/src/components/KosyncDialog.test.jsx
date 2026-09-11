import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { t } from "../i18n";
import KosyncDialog from "./KosyncDialog";

vi.mock("../api/kosync", () => ({ fetchKosync: vi.fn(), setKosyncPassword: vi.fn() }));
import { fetchKosync, setKosyncPassword } from "../api/kosync";

describe("KosyncDialog", () => {
  it("shows the setup steps and saves a device password", async () => {
    fetchKosync.mockResolvedValue({ enabled: false, login: "reader" });
    setKosyncPassword.mockResolvedValue({ enabled: true });
    const onClose = vi.fn();
    render(<KosyncDialog onClose={onClose} />);
    expect(await screen.findByText(t("kosync.off"))).toBeInTheDocument();
    expect(screen.getByText("reader")).toBeInTheDocument();

    const input = screen.getByPlaceholderText(t("kosync.password"));
    fireEvent.change(input, { target: { value: "short" } });
    fireEvent.click(screen.getByRole("button", { name: t("kosync.save") }));
    expect(screen.getByText(t("kosync.tooShort"))).toBeInTheDocument();
    expect(setKosyncPassword).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: "device-pass-123" } });
    fireEvent.click(screen.getByRole("button", { name: t("kosync.save") }));
    await waitFor(() => expect(screen.getByText(t("kosync.on"))).toBeInTheDocument());
    expect(setKosyncPassword).toHaveBeenCalledWith("device-pass-123");

    fireEvent.click(screen.getByRole("button", { name: t("kosync.close") }));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("disables sync", async () => {
    fetchKosync.mockResolvedValue({ enabled: true, login: "reader" });
    setKosyncPassword.mockResolvedValue({ enabled: false });
    render(<KosyncDialog onClose={() => {}} />);
    fireEvent.click(await screen.findByRole("button", { name: t("kosync.disable") }));
    await waitFor(() => expect(screen.getByText(t("kosync.off"))).toBeInTheDocument());
    expect(setKosyncPassword).toHaveBeenCalledWith("");
  });
});
