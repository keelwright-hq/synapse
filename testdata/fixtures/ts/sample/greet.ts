import { readFile } from "fs";

export type User = {
  id: string;
  name: string;
};

export function greet(user: User): string {
  readFile("x");
  return format(user.name);
}

export const format = (name: string): string => {
  return name.toUpperCase();
};

export class Greeter {
  say(name: string): string {
    return format(name);
  }
}
