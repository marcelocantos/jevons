// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

export function FrontierTable(props: { rows: { id: string; name: string }[] }) {
  return (
    <table className="frontier-table">
      <thead>
        <tr>
          <th>id</th>
          <th>name</th>
        </tr>
      </thead>
      <tbody>
        {props.rows.map((r) => (
          <tr key={r.id}>
            <td>{r.id}</td>
            <td>{r.name}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
