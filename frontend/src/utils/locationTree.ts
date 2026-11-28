export type LocationNode = { ID: string; Name: string; children: LocationNode[] };
export type LocationRow = { id: string; name: string; path: string; depth: number; hasKids: boolean };

/**
 * Tree rows in display order. Without a query only children of expanded nodes are listed;
 * with a query, every location whose full path contains all words is listed.
 */
export function locationRows(tree: LocationNode[], expanded: Set<string>, query = ""): LocationRow[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const out: LocationRow[] = [];
  const walk = (nodes: LocationNode[], prefix: string, depth: number) => {
    for (const n of nodes) {
      const path = prefix + n.Name;
      if (words.every((w) => path.toLowerCase().includes(w))) {
        out.push({ id: n.ID, name: n.Name, path, depth, hasKids: n.children.length > 0 });
      }
      if (words.length || expanded.has(n.ID)) walk(n.children, path + " / ", depth + 1);
    }
  };
  walk(tree, "", 0);
  return out;
}

/** Nodes from the root down to `id`, or [] if the id is not in the tree. */
export function locationChain(tree: LocationNode[], id: string): LocationNode[] {
  for (const n of tree) {
    if (n.ID === id) return [n];
    const below = locationChain(n.children, id);
    if (below.length) return [n, ...below];
  }
  return [];
}
