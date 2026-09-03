const fs = require("fs");
import { readFile } from "fs/promises";

export class User {}

export function greet(name) {
  readFile("x");
  return format(name);
}

export const format = (name) => {
  return name.toUpperCase();
};

export class Greeter {
  say(name) {
    return format(name);
  }
}
