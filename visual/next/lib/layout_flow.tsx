'use client';
import dagre from '@dagrejs/dagre';
import { Node, Edge } from 'reactflow';

const g = new dagre.graphlib.Graph();

export function layoutFlow(nodes: Node<any>[], edges: Edge<any>[], direction: string) {
  g.setGraph({ rankdir: direction, nodesep: 80, marginy: 50, ranksep: 80});
  g.setDefaultEdgeLabel(() => ({}));

  edges.forEach((edge) => g.setEdge(edge.source, edge.target));
  nodes.forEach((node) => g.setNode(node.id, node.data));

  dagre.layout(g);

  return {
    nodes: nodes.map((node) => {
      const { x, y } = g.node(node.id);

      return { ...node, position: { x, y } };
    }),
    edges,
  };
};
