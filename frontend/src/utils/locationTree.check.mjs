// Self-check, no test runner needed: node src/utils/locationTree.check.mjs (Node 22.18+)
import assert from "node:assert/strict";
import { locationChain, locationRows } from "./locationTree.ts";

const node = (ID, Name, children = []) => ({ ID, Name, children });
const tree = [node("1", "Keller", [node("2", "Regal", [node("3", "Kiste")])]), node("4", "Büro", [node("5", "Regal")])];
const ids = (rows) => rows.map((r) => r.id).join();

assert.equal(ids(locationRows(tree, new Set())), "1,4");
assert.equal(ids(locationRows(tree, new Set(["1"]))), "1,2,4");
assert.equal(ids(locationRows(tree, new Set(["2"]))), "1,4", "collapsed parent hides expanded child");
assert.equal(ids(locationRows(tree, new Set(), "keller REGAL")), "2,3");
assert.equal(locationRows(tree, new Set(), "kiste")[0].path, "Keller / Regal / Kiste");
assert.equal(locationChain(tree, "3").map((n) => n.ID).join(), "1,2,3");
assert.deepEqual(locationChain(tree, "nope"), []);
console.log("locationTree ok");
