import React from "react";

export type Props = { label: string };

export function Badge(props: Props) {
  return <span>{props.label}</span>;
}

export const App = () => {
  return <Badge label={format("ok")} />;
};

function format(s: string): string {
  return s.trim();
}
