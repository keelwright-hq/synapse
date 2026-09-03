import React from "react";

export function Badge({ label }) {
  return <span>{label}</span>;
}

export class App {
  render() {
    return <Badge label="hi" />;
  }
}
