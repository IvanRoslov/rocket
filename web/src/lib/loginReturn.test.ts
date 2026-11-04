import { describe, expect, it } from "vitest";
import { returnAfterLogin } from "./loginReturn";

describe("login across application entries", () => {
  it("loads the separate creative app with the complete deep link", () => {
    const routed: string[] = [],
      loaded: string[] = [];
    returnAfterLogin(
      "/creative/runs/17?stage=4",
      (p) => routed.push(p),
      (p) => loaded.push(p),
    );
    expect(loaded).toEqual(["/creative/runs/17?stage=4"]);
    expect(routed).toEqual([]);
  });
  it("keeps development return paths within its router", () => {
    const routed: string[] = [],
      loaded: string[] = [];
    returnAfterLogin(
      "/p/rocket",
      (p) => routed.push(p),
      (p) => loaded.push(p),
    );
    expect(routed).toEqual(["/p/rocket"]);
    expect(loaded).toEqual([]);
  });
});
