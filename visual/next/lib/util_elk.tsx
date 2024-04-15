'use client';

import { ElkNode, ElkExtendedEdge } from 'elkjs/lib/elk.bundled.js'
import { Node, Edge } from 'reactflow';

let rootID = 'root';

interface traverseOption {
    elkNode: ElkNode,
    parentNode?: ElkNode,
    zIndex?: number,
}
export function traverseElk({ elkNode, parentNode, zIndex = 0 }: traverseOption, onNode: (opt: traverseOption) => void,) {
    onNode({ elkNode, parentNode, zIndex })
    elkNode.children && elkNode.children.forEach((child) => traverseElk({
        elkNode: child,
        parentNode: elkNode,
        zIndex: zIndex + 1,
    }, onNode));
}
export function fromElk(elkNode: ElkNode): { nodes: Node[], edges: Edge[] } {
    let nodes: Node[] = [];
    let edges: Edge[] = [];
    traverseElk(
        { elkNode },
        ({ elkNode, parentNode, zIndex = 0 }: traverseOption) => {
            if (elkNode.id !== rootID) {
                nodes.push({
                    id: elkNode.id,
                    data: { label: elkNode.labels && elkNode.labels[0].text },
                    position: { x: elkNode.x || 0, y: elkNode.y || 0 },
                    parentId: parentNode && parentNode.id !== rootID ? parentNode.id : undefined,
                    zIndex: zIndex,
                });
            }
            elkNode.edges && elkNode.edges.forEach((edge: ElkExtendedEdge) => {
                edges.push({
                    id: edge.id,
                    source: edge.sources[0],
                    target: edge.targets[0],
                    zIndex: zIndex + 1,
                });
            });
        },
    )
    return { nodes, edges }
}