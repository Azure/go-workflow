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

export function toElk({ nodes, edges }: { nodes: Node[], edges: Edge[] }): ElkNode {
    let rootElkNode: ElkNode = { id: rootID, children: [], edges: [] }
    let allElkNodes: Map<string, ElkNode> = new Map();
    let whoIsParent: Map<string, string> = new Map();
    allElkNodes.set(rootID, rootElkNode)
    nodes.forEach((node) => {
        let elkNode = allElkNodes.get(node.id)
        if (!elkNode) {
            elkNode = {
                id: node.id,
                x: node.position.x,
                y: node.position.y,
                width: node.width === null ? undefined : node.width,
                height: node.height === null ? undefined : node.height,
                labels: [{text: node.data.label }],
                children: [],
                edges: [],
            }
            allElkNodes.set(node.id, elkNode)
        }
        let parentElkNode: ElkNode | undefined
        let parentId = node.parentId || rootID
        whoIsParent.set(node.id, parentId)
        parentElkNode = allElkNodes.get(parentId)
        if (!parentElkNode) {
            parentElkNode = { id: parentId, children: [] }
            allElkNodes.set(parentId, parentElkNode)
        }
        parentElkNode?.children?.push(elkNode)
    })
    edges.forEach((edge) => {
        let sourceNode = allElkNodes.get(edge.source)
        let targetNode = allElkNodes.get(edge.target)
        if (sourceNode && targetNode) {
            let elkEdge: ElkExtendedEdge = {
                id: edge.id,
                sources: [sourceNode.id],
                targets: [targetNode.id],
            }
            let parent = whoIsParent.get(edge.source)
            if (parent) {
                let parentNode = allElkNodes.get(parent)
                if (parentNode) {
                    parentNode.edges?.push(elkEdge)
                }
            }
        }
    })
    return rootElkNode
}