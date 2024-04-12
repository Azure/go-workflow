import { memo } from 'react';
import { NodeProps, NodeResizer, Handle, Position } from 'reactflow';
// import { Card, CardHeader, Divider, Spinner } from '@nextui-org/react';

function SubFlow(p: NodeProps) {
    return (
        <>
            <NodeResizer isVisible={p.selected} />
            <Handle type='target' position={p.targetPosition || Position.Top} />
            {/* <Card> */}
                {/* <CardHeader className="flex gap-3"> */}
                    {/* <Spinner size='sm' label="Running" color="primary" labelColor="primary"/> */}
                    <div className="flex flex-col">
                        <p className="text-md">{p.data.label}</p>
                    </div>
                    {/* <Divider/> */}
                {/* </CardHeader> */}
            {/* </Card> */}
            <Handle type='source' position={p.sourcePosition || Position.Bottom} />
        </>
    );
}

export default memo(SubFlow);
