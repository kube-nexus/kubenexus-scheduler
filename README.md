Currently has basic logic to support MPI/AI workload using the coscheduling plugin. 
Gang-scheduling in Spark pods is achieved using the modified extender [https://github.paypal.com/gkotapalle/modified-extender](https://github.paypal.com/gkotapalle/modified-extender)

To achieve the co-existence of Spark workload along with other workload while taking advantage of FIFO Gang scheduling for Spark Jobs, we use the modified extender's resource reservations CRD objects and extend it to work with non-Spark workload as well. We use the preExtender custom plugin to achieve the same. 

The **preExtender** plugin makes sure that a unique reservation object is created for all the pods in a deployment/replicaset.

The **resourceReservation** plugin creates a reservation using resourceReservation CRD from palantir. The reservation is created for the node that is selected after passing through the predicates, extender predicate and scoring stages.



